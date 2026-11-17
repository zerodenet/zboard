#!/usr/bin/env python3
"""Real Chromium + isolated SQLite/HTTP session acceptance; never targets an existing server.

Requires Python Playwright and an installed Chromium. No repository dependencies
are installed. Credentials, screenshots and raw diagnostics stay in a fresh 0700
directory under .codex-local-artifacts; stdout contains only sanitized results.
"""
import argparse
import base64
import hashlib
import hmac
import json
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import subprocess
import tempfile
import time
import traceback
from urllib.parse import urlparse
from urllib.request import urlopen

from playwright.sync_api import sync_playwright


class CheckFailure(Exception):
    pass


def check(name, condition):
    if not condition:
        raise CheckFailure(name)


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def manifest(directory):
    rows = [(str(path.relative_to(directory)), digest(path)) for path in sorted(directory.rglob('*')) if path.is_file()]
    return {'sha256': hashlib.sha256(json.dumps(rows, separators=(',', ':')).encode()).hexdigest(), 'files': rows}


def private_json(path, data):
    path.write_text(json.dumps(data, indent=2) + '\n')
    path.chmod(0o600)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--server-binary', type=Path, required=True)
    parser.add_argument('--seed-binary', type=Path, required=True)
    parser.add_argument('--backend-source', type=Path, required=True, help='checkout used to build both executables')
    parser.add_argument('--web-dir', type=Path, required=True)
    parser.add_argument('--chromium', default='/usr/bin/chromium')
    parser.add_argument('--evidence-root', type=Path, help='new private run children under this explicit evidence parent')
    args = parser.parse_args()
    os.umask(0o077)
    root = Path(__file__).resolve().parent.parent
    evidence_root = (args.evidence_root or root / '.codex-local-artifacts' / 'acceptance').resolve()
    evidence_root.mkdir(mode=0o700, parents=True, exist_ok=True)
    evidence = Path(tempfile.mkdtemp(prefix='session-browser-', dir=evidence_root))
    data = evidence / 'data'
    backend_commit = subprocess.check_output(['git', '-C', str(args.backend_source), 'rev-parse', 'HEAD'], text=True).strip()
    result = {
        'backend_source_commit': backend_commit,
        'frontend_source_commit': subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip(),
        'server_binary_sha256': digest(args.server_binary),
        'seed_binary_sha256': digest(args.seed_binary),
        'script_sha256': digest(Path(__file__)),
        'frontend_source_manifest': manifest(root / 'frontend' / 'src'),
        'checks': {}, 'failed_checks': [], 'stage': 'frontend_snapshot', 'passed': False,
        'limits': ['isolated SQLite only', 'no external identity provider', 'not production acceptance', 'no capacity claim'],
    }
    server = None
    private_log = (evidence / 'server.log').open('w')
    try:
        web_snapshot = evidence / 'web'
        web_before = manifest(args.web_dir.resolve())
        shutil.copytree(args.web_dir.resolve(), web_snapshot)
        result['frontend_dist_manifest'] = manifest(web_snapshot)
        check('stable_frontend_snapshot', web_before == result['frontend_dist_manifest'] == manifest(args.web_dir.resolve()))
        result['stage'] = 'seed'
        subprocess.run([str(args.seed_binary.resolve()), '-mode', 'seed', '-dir', str(data), '-nodes', '1', '-subscriptions', '3', '-records', '0'], check=True, stdout=private_log, stderr=subprocess.STDOUT)
        fixture = json.loads((data / 'fixture.json').read_text())
        check('fresh_isolated_fixture', bool(fixture.get('run_id')) and data.parent == evidence)
        database = data / 'zboard.db'
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        origin = f'http://127.0.0.1:{port}'
        config = json.loads((data / 'zboard.json').read_text())
        config.update({'Host': '127.0.0.1', 'Port': port, 'datasource': str(database), 'zero_event_spool_dir': str(data / 'events'), 'zero_artifact_dir': str(data / 'artifacts'), 'file_storage_dir': str(data / 'files'), 'plugins': {'directory': str(data / 'plugins')}})
        private_json(data / 'zboard.json', config)
        env = {key: value for key, value in os.environ.items() if not key.startswith('ZBOARD_')}
        env.update({'GOMAXPROCS': '1', 'ZBOARD_WEB_DIR': str(web_snapshot)})
        server = subprocess.Popen([str(args.server_binary.resolve()), '-f', str(data / 'zboard.json')], cwd=data, env=env, stdout=private_log, stderr=subprocess.STDOUT)
        for _ in range(100):
            if server.poll() is not None:
                raise CheckFailure('isolated_server_started')
            try:
                with urlopen(origin + '/api/v1/setup/status', timeout=1) as response:
                    if response.status == 200:
                        break
            except OSError:
                time.sleep(.1)
        else:
            raise CheckFailure('isolated_server_ready')

        def sql(query, parameters=()):
            with sqlite3.connect(database, timeout=10) as db:
                return db.execute(query, parameters).fetchall()

        emails = {row[0]: row[1] for row in sql('SELECT id,email FROM users WHERE id IN (1,2,3)')}
        check('fixture_only_accounts', len(emails) == 3 and emails[1] == fixture['admin_email'])
        original_site_name = sql('SELECT site_name FROM installations WHERE id=1')[0][0]

        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(executable_path=args.chromium, headless=True, chromium_sandbox=True, args=['--disable-dev-shm-usage', '--disable-background-networking', '--no-first-run', '--disable-component-update'])
            result['chromium_version'] = browser.version
            requests = []
            external_blocked = []
            gate = {'targets': set(), 'held': [], 'statuses': []}

            def route_request(route):
                parsed = urlparse(route.request.url)
                if f'{parsed.scheme}://{parsed.netloc}' != origin:
                    external_blocked.append({'host': parsed.hostname, 'resource_type': route.request.resource_type})
                    route.abort()
                    return
                identity = (route.request.method, parsed.path)
                requests.append(identity)
                if identity in gate['targets']:
                    # Delay actual server responses so both genuine 401s reach
                    # the browser. Never fabricate an API response or token fact.
                    response = route.fetch(max_redirects=0)
                    gate['held'].append((route, response))
                    gate['statuses'].append(response.status)
                    if len(gate['held']) == 2:
                        gate['targets'] = set()
                        held, gate['held'] = gate['held'], []
                        for pending, actual in sorted(held, key=lambda pair: pair[0].request.method != 'GET'):
                            pending.fulfill(response=actual)
                            actual.dispose()
                    return
                route.continue_()

            def context():
                value = browser.new_context(viewport={'width': 1280, 'height': 900}, service_workers='block')
                value.route('**/*', route_request)
                return value

            def login(page, account):
                page.goto(origin + '/login')
                page.locator('#login-email').fill(emails[account])
                page.locator('#login-password').fill(fixture['admin_password'])
                page.get_by_role('button', name='登录', exact=True).click()
                page.wait_for_url('**/admin/dashboard' if account == 1 else '**/account')

            def expire(page):
                token = page.evaluate("localStorage.getItem('zboard.auth.token')")
                check('current_fixture_token_present', bool(token))
                encoded = token.split('.')[0]
                claims = json.loads(base64.urlsafe_b64decode(encoded + '=' * (-len(encoded) % 4)))
                claims['exp'] = int(time.time()) - 60
                payload = json.dumps(claims, separators=(',', ':')).encode()
                encode = lambda value: base64.urlsafe_b64encode(value).rstrip(b'=').decode()
                expired = encode(payload) + '.' + encode(hmac.new(fixture['jwt_secret'].encode(), payload, hashlib.sha256).digest())
                page.evaluate("token => localStorage.setItem('zboard.auth.token', token)", expired)

            def trigger(page, method, path, selector):
                page.bring_to_front()
                gate.update({'targets': {(method, path), ('GET', '/api/v1/navigation')}, 'held': [], 'statuses': []})
                expire(page)
                page.evaluate("selector => { document.querySelector(selector).closest('form').requestSubmit(); window.dispatchEvent(new Event('focus')); }", selector)
                dialog = page.get_by_role('dialog', name='登录已过期', exact=True)
                dialog.wait_for(state='visible')
                check('two_real_http_401_responses', sorted(gate['statuses']) == [401, 401])
                check('one_reauthentication_dialog', page.get_by_role('dialog', name='登录已过期', exact=True).count() == 1)
                return dialog

            def reauthenticate(page, dialog, account):
                dialog.locator('#reauth-email').fill(emails[account])
                dialog.locator('#reauth-password').fill(fixture['admin_password'])
                dialog.get_by_role('button', name='重新认证', exact=True).click()
                dialog.wait_for(state='hidden')
                page.wait_for_timeout(400)

            def geometry(page, editor):
                return editor.evaluate("""element => {
                    const root = document.scrollingElement;
                    const describe = node => ({ tag: node.tagName, id: node.id, scrollTop: node.scrollTop, scrollHeight: node.scrollHeight, clientHeight: node.clientHeight, maxScroll: node.scrollHeight - node.clientHeight });
                    const containers = [];
                    for (let node = element.parentElement; node; node = node.parentElement) {
                        if (node.scrollHeight > node.clientHeight && ['auto', 'scroll'].includes(getComputedStyle(node).overflowY)) containers.push(describe(node));
                    }
                    return { windowScrollY: window.scrollY, document: describe(root), focus: {tag: document.activeElement?.tagName, id: document.activeElement?.id}, containers };
                }""")

            result['stage'] = 'same_account_settings'
            admin_context = context()
            admin = admin_context.new_page()
            login(admin, 1)
            admin.goto(origin + '/admin/settings/site?acceptance=session#draft')
            editor = admin.locator('#settings-site-name')
            editor.wait_for(state='visible')
            admin.wait_for_load_state('networkidle')
            editor.fill('browser-unsaved-settings')
            editor.evaluate("element => { element.dataset.acceptanceEditor = 'settings'; element.focus(); }")
            admin.evaluate('window.scrollTo(0, 350)')
            scroll = admin.evaluate('window.scrollY')
            geometry_before = geometry(admin, editor)
            original_url = admin.url
            count = requests.count(('PUT', '/api/v1/admin/settings'))
            dialog = trigger(admin, 'PUT', '/api/v1/admin/settings', '#settings-site-name')
            check('settings_original_url_preserved', admin.url == original_url)
            check('settings_original_dom_preserved', editor.get_attribute('data-acceptance-editor') == 'settings')
            check('background_inert', editor.evaluate('element => Boolean(element.closest("[inert]"))'))
            check('dialog_outside_inert_boundary', dialog.evaluate('element => !element.closest("[inert]")'))
            check('dialog_initial_focus', dialog.evaluate('element => element.contains(document.activeElement)'))
            editor.evaluate('element => element.focus()')
            check('inert_prevents_background_focus', dialog.evaluate('element => element.contains(document.activeElement)'))
            for _ in range(8):
                admin.keyboard.press('Tab')
                check('dialog_tab_focus_trapped', dialog.evaluate('element => element.contains(document.activeElement)'))
            admin.keyboard.press('Escape')
            check('escape_does_not_dismiss_reauthentication', dialog.is_visible())
            admin.screenshot(path=str(evidence / 'same-account-dialog.png'))
            reauthenticate(admin, dialog, 1)
            check('settings_same_account_draft_preserved', editor.input_value() == 'browser-unsaved-settings')
            check('settings_same_account_dom_preserved', editor.get_attribute('data-acceptance-editor') == 'settings')
            check('settings_same_account_url_preserved', admin.url == original_url)
            check('settings_background_focus_restored', editor.evaluate('element => document.activeElement === element'))
            restored_scroll = admin.evaluate('window.scrollY')
            geometry_after = geometry(admin, editor)
            scroll_preserved = abs(restored_scroll - scroll) <= 1
            if not scroll_preserved:
                result['failed_checks'].append('settings_scroll_preserved')
            check('settings_write_not_replayed', requests.count(('PUT', '/api/v1/admin/settings')) - count == 1)
            check('expired_write_did_not_mutate_setting', sql('SELECT site_name FROM installations WHERE id=1')[0][0] == original_site_name)
            result['checks']['same_account_settings'] = {'passed': scroll_preserved, 'real_401_responses': 2, 'write_attempts': 1, 'draft_dom_url_focus_preserved': True, 'scroll_before': scroll, 'scroll_after': restored_scroll, 'scroll_preserved': scroll_preserved, 'geometry_before': geometry_before, 'geometry_after': geometry_after, 'inert_and_keyboard_focus_trap': True}

            result['stage'] = 'financial_write_not_replayed'
            account_context = context()
            account = account_context.new_page()
            login(account, 2)
            account.goto(origin + '/account/wallet?currency=CNY#draft')
            amount = account.locator('#wallet-topup-amount')
            amount.fill('9.00')
            amount.evaluate("element => { element.dataset.acceptanceEditor = 'wallet'; element.focus(); }")
            account_url = account.url
            count = requests.count(('POST', '/api/v1/account/wallet/topups'))
            dialog = trigger(account, 'POST', '/api/v1/account/wallet/topups', '#wallet-topup-amount')
            reauthenticate(account, dialog, 2)
            check('wallet_same_account_draft_preserved', amount.input_value() == '9.00' and amount.get_attribute('data-acceptance-editor') == 'wallet')
            check('wallet_original_url_preserved', account.url == account_url)
            check('financial_write_not_replayed', requests.count(('POST', '/api/v1/account/wallet/topups')) - count == 1)
            check('no_topup_or_wallet_entry_created', sql("SELECT count(*) FROM orders WHERE order_type='topup'")[0][0] == 0 and sql('SELECT count(*) FROM wallet_entries')[0][0] == 0)
            result['checks']['financial_write_not_replayed'] = {'passed': True, 'real_401_responses': 2, 'topup_post_attempts': 1, 'new_orders': 0, 'new_wallet_entries': 0}

            result['stage'] = 'account_switch_isolation'
            dialog = trigger(account, 'POST', '/api/v1/account/wallet/topups', '#wallet-topup-amount')
            reauthenticate(account, dialog, 1)
            amount = account.locator('#wallet-topup-amount')
            check('account_switch_keeps_route_with_fresh_context', account.url == account_url)
            check('account_switch_discards_old_draft', amount.input_value() == '' and amount.get_attribute('data-acceptance-editor') is None)
            check('account_switch_does_not_replay_old_checkout', requests.count(('POST', '/api/v1/account/wallet/topups')) - count == 2)
            result['checks']['account_switch_isolation'] = {'passed': True, 'same_route_fresh_form': True, 'previous_checkout_not_replayed': True}

            result['stage'] = 'same_account_admin_revocation'
            admin.bring_to_front()
            admin.wait_for_load_state('networkidle')
            sql('UPDATE users SET is_admin=0 WHERE id=1')
            dialog = trigger(admin, 'PUT', '/api/v1/admin/settings', '#settings-site-name')
            reauthenticate(admin, dialog, 1)
            admin.wait_for_url('**/account')
            check('revoked_admin_editor_removed', admin.locator('[data-acceptance-editor="settings"]').count() == 0)
            check('revoked_admin_draft_not_exposed', 'browser-unsaved-settings' not in admin.locator('body').inner_text())
            credential = admin.evaluate("localStorage.getItem('zboard.auth.token')")
            response = admin_context.request.get(origin + '/api/v1/admin/system-configs', headers={'Authorization': 'Bearer ' + credential})
            check('revoked_admin_get_is_forbidden', response.status == 403)
            response = admin_context.request.get(origin + '/api/v1/auth/me', headers={'Authorization': 'Bearer ' + credential})
            check('revoked_admin_session_still_valid', response.status == 200)
            check('current_permissions_refreshed', response.json()['data']['id'] == 1 and response.json()['data']['is_admin'] is False)
            result['checks']['same_account_admin_revocation'] = {'passed': True, 'new_identity_http_status': 200, 'admin_endpoint_http_status': 403, 'old_editor_and_draft_removed': True}
            result['blocked_other_origin_attempts'] = external_blocked
            result['browser_network_policy'] = 'Only this isolated loopback origin is forwarded; every other page-request origin is aborted.'
            result['external_requests_sent'] = 0
            account_context.close()
            admin_context.close()
            browser.close()
        result['stage'] = 'complete'
        result['passed'] = len(result['checks']) == 4 and all(value['passed'] for value in result['checks'].values())
    except Exception as error:
        result['error_type'] = type(error).__name__
        if isinstance(error, CheckFailure):
            result['failed_check'] = str(error)
            result['failed_checks'].append(str(error))
        (evidence / 'private-failure.txt').write_text(traceback.format_exc())
    finally:
        if server is not None:
            server.terminate()
            try:
                server.wait(timeout=15)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait(timeout=5)
            result['server_exit_code'] = server.returncode
        private_log.close()
        private_json(evidence / 'result.json', result)
    print(json.dumps({'passed': result['passed'], 'stage': result['stage'], 'failed_checks': result['failed_checks'], 'error_type': result.get('error_type'), 'evidence_directory': str(evidence)}))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
