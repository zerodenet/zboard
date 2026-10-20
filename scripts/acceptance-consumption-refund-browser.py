#!/usr/bin/env python3
"""Opt-in loopback Chromium driver for TestConsumptionRefundChromiumAcceptance.

Owner and administrator use isolated BrowserContexts and actual login/HTTP/UI.
Coordination files request only evidence checks. Responses are never fulfilled or
fabricated. Same-key replays are explicit test HTTP actions, not automatic UI writes.
"""
import argparse
from collections import Counter
import json
import os
from pathlib import Path
import stat
import time
import traceback
from urllib.parse import quote, urlparse

from playwright.sync_api import sync_playwright, expect


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture', type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    fixture = json.loads(args.fixture.read_text())
    evidence = Path(fixture['evidence_dir'])
    mode = evidence.lstat().st_mode
    if not evidence.is_absolute() or not stat.S_ISDIR(mode) or stat.S_IMODE(mode) != 0o700:
        raise RuntimeError('Evidence must be a private 0700 directory, not a symlink')
    if args.fixture.resolve().parent != evidence.resolve():
        raise RuntimeError('Fixture must belong to its private evidence directory')
    for entry in evidence.iterdir():
        if entry.name.endswith(('.request', '.done', '.png', '-private.html')) or entry.name in ('browser-checks.json', 'financial-checks.json'):
            raise RuntimeError('Fresh evidence directory required')
    def write_private(name, value):
        raw = value if isinstance(value, bytes) else value.encode('utf-8')
        with (evidence / name).open('xb') as output:
            output.write(raw)

    def screenshot(page, name):
        write_private(name, page.screenshot(full_page=True))

    origin = fixture['origin']
    parsed_origin = urlparse(origin)
    if parsed_origin.scheme != 'http' or parsed_origin.hostname != '127.0.0.1':
        raise RuntimeError('Only isolated loopback HTTP is allowed')
    order = fixture['order_id']
    subscription = fixture['subscription_id']
    refund_path = f'/api/v1/admin/orders/{order}/consumption-refunds'
    subject = '核对已结束订阅的消费退款'
    reason = '依据原账户工单核准已结束订阅消费退款'
    result = {'passed': False, 'checks': {}, 'stage': 'launch', 'limits': [
        'isolated SQLite and selected real handlers; not full router/middleware acceptance',
        'authorized test PaymentFact purchase; no live external gateway transaction',
        'explicit future clock passed to expiry; not natural-term passage',
        'closed source-order approval only; active partial-refund policy remains unsupported',
        'known-success exact-key HTTP replay; no browser unknown-outcome fault injection',
        'no API mocks, no external websites, no capacity claim']}
    requests, failures, blocked = [], [], []
    auth = {}
    pages = {}

    def check(name, condition):
        result['checks'][name] = bool(condition)
        if not condition:
            raise AssertionError(name)

    def stage(page, name, response):
        write_private(name + '.request', 'ready')
        deadline = time.monotonic() + 60
        while not (evidence / (response + '.done')).exists():
            if time.monotonic() >= deadline:
                raise TimeoutError('Go evidence check: ' + response)
            page.wait_for_timeout(100)

    def writes(role=None, path=None):
        return [request for request in requests if request['method'] not in ('GET', 'HEAD')
                and (role is None or request['role'] == role)
                and (path is None or request['path'] == path)]

    def api(page, role, method, path, payload=None):
        check('actual_auth_available_' + role, bool(auth.get(role)))
        if not path.startswith('/api/v1/'):
            raise RuntimeError('Only local actual API calls are permitted')
        return page.evaluate('''async ({method, path, payload, authorization}) => {
            const options = {method, headers: {Authorization: authorization}};
            if (payload !== null) {
                options.headers['Content-Type'] = 'application/json';
                options.body = JSON.stringify(payload);
            }
            const response = await fetch(path, options);
            return {status: response.status, body: await response.json()};
        }''', {'method': method, 'path': path, 'payload': payload, 'authorization': auth[role]})

    p = None
    try:
        p = sync_playwright().start()
        browser = p.chromium.launch(executable_path='/usr/bin/chromium', headless=True,
            args=['--no-sandbox', '--disable-dev-shm-usage', '--disable-background-networking',
                  '--disable-component-update', '--no-first-run'])
        result['chromium_version'] = browser.version

        def open_context(role):
            context = browser.new_context(viewport={'width': 1440, 'height': 1100}, service_workers='block')

            def route_request(route):
                url = urlparse(route.request.url)
                if f'{url.scheme}://{url.netloc}' != origin:
                    blocked.append({'role': role, 'method': route.request.method,
                                    'url': route.request.url, 'blocked_before_network': True})
                    route.abort()
                    return
                route.continue_()

            def request_seen(request):
                path = urlparse(request.url).path
                if not path.startswith('/api/'):
                    return
                if request.headers.get('authorization'):
                    auth[role] = request.headers['authorization']
                data = {'role': role, 'method': request.method, 'path': path}
                if request.method == 'POST' and path in (refund_path, '/api/v1/tickets'):
                    data['payload'] = request.post_data_json
                requests.append(data)

            def response_seen(response):
                if urlparse(response.url).path.startswith('/api/') and response.status >= 400:
                    failures.append({'role': role, 'path': urlparse(response.url).path, 'status': response.status})

            context.route('**/*', route_request)
            context.on('request', request_seen)
            context.on('response', response_seen)
            page = context.new_page()
            page.set_default_timeout(20000)
            pages[role] = page
            return context, page

        owner_context, owner = open_context('owner')
        admin_context, admin = open_context('admin')
        check('separate_browser_contexts', owner_context != admin_context)

        def login(page, role, email, redirect):
            page.goto(origin + '/login?redirect=' + quote(redirect, safe=''))
            page.locator('#login-email').fill(email)
            page.locator('#login-password').fill(fixture['password'])
            with page.expect_response(lambda response: urlparse(response.url).path == '/api/v1/auth/login') as response:
                page.get_by_role('button', name='登录', exact=True).click()
            check('real_login_' + role, response.value.status == 200)
            auth[role] = 'Bearer ' + response.value.json()['data']['auth']['token']
            page.wait_for_url(origin + redirect)
            identity = api(page, role, 'GET', '/api/v1/auth/me')
            check('real_role_' + role, identity['status'] == 200
                  and identity['body']['data']['id'] == fixture[role + '_id']
                  and bool(identity['body']['data'].get('is_admin')) == (role == 'admin'))

        result['stage'] = 'owner_historical_order'
        login(owner, 'owner', fixture['owner_email'], f'/account/orders/{order}')
        expect(owner.get_by_role('heading', name=f'订单 #{order}', exact=True)).to_be_visible()
        expect(owner.locator('main')).to_contain_text('原订阅已到期')
        screenshot(owner, '01-owner-ended-order.png')
        result['stage'] = 'owner_ticket'
        owner.get_by_role('link', name='就此订单提交工单', exact=True).click()
        expect(owner.locator('#create-ticket-form')).to_be_visible()
        expect(owner.locator('#ticket-order_id')).to_have_value(str(order))
        check('ticket_context_never_auto_submits', len(writes(path='/api/v1/tickets')) == 0)
        owner.locator('#ticket-subscription_id').fill(str(subscription))
        owner.locator('#ticket-subject').fill(subject)
        owner.locator('#ticket-body').fill('原订阅已结束，请核对原支付与退款依据。本工单只提供原账户记录。')
        with owner.expect_response(lambda response: urlparse(response.url).path == '/api/v1/tickets'
                and response.request.method == 'POST') as response:
            owner.get_by_role('button', name='提交工单', exact=True).click()
        check('real_owner_ticket_created', response.value.status == 200)
        ticket = response.value.json()['data']['ticket']
        ticket_id = ticket['id']
        check('ticket_owned_references', ticket['user_id'] == fixture['owner_id']
              and ticket['references'] == {'order_id': order, 'subscription_id': subscription}
              and len(writes(path='/api/v1/tickets')) == 1)
        expect(owner.get_by_role('heading', name=subject, exact=True)).to_be_visible()
        expect(owner.get_by_label('关联记录', exact=True)).to_contain_text(str(order))
        screenshot(owner, '02-owner-persisted-ticket.png')
        refusal = api(owner, 'owner', 'POST', refund_path,
            {'amount_minor': 300, 'ticket_id': ticket_id, 'reason': reason,
             'idempotency_key': 'browser-owner-cannot-approve'})
        check('owner_cannot_approve_admin_refund', refusal['status'] == 403)
        stage(owner, 'ticket', 'ticket_checked')

        result['stage'] = 'admin_same_owner_ticket_boundary'
        login(admin, 'admin', fixture['admin_email'], f'/admin/orders?order={order}&user_id=1')
        check('isolated_login_tokens', auth['owner'] != auth['admin'])
        form = admin.locator('form[data-test="consumption-refund"]')
        expect(form).to_be_visible()
        amount_input = admin.locator(f'#consumption-refund-amount-{order}')
        ticket_input = admin.locator(f'#consumption-refund-ticket-{order}')
        reason_input = admin.locator(f'#consumption-refund-reason-{order}')
        amount_input.fill('3.00')
        ticket_input.fill(str(fixture['foreign_ticket_id']))
        reason_input.fill(reason)

        def approve(expected_status):
            before = len(writes('admin', refund_path))
            form.get_by_role('button', name='审批退款至余额', exact=True).click()
            dialog = admin.get_by_role('alertdialog')
            expect(dialog).to_contain_text('CNY 3.00')
            expect(dialog).to_contain_text(f'用户 #{fixture["owner_id"]}')
            check('confirmation_before_post_' + str(before), len(writes('admin', refund_path)) == before)
            with admin.expect_response(lambda response: urlparse(response.url).path == refund_path
                    and response.request.method == 'POST') as response:
                dialog.get_by_role('button', name='确认退款至余额', exact=True).click()
            check('explicit_approval_status_' + str(before + 1), response.value.status == expected_status
                  and len(writes('admin', refund_path)) == before + 1)
            return response.value.json()

        rejected = approve(400)
        check('foreign_owner_ticket_rejected', '工单' in rejected.get('message', ''))
        expect(form.get_by_role('button', name='修改被拒绝的申请', exact=True)).to_be_visible()
        stage(admin, 'rejected', 'rejection_checked')
        form.get_by_role('button', name='修改被拒绝的申请', exact=True).click()
        ticket_input.fill(str(ticket_id))
        result['stage'] = 'actual_admin_approval'
        approved = approve(200)['data']
        good_request = dict(writes('admin', refund_path)[-1]['payload'])
        check('only_approval_references_and_amount', set(good_request) == {
            'amount_minor', 'ticket_id', 'reason', 'idempotency_key'})
        check('corrected_intent_requires_new_explicit_key',
            good_request['idempotency_key'] != writes('admin', refund_path)[0]['payload']['idempotency_key'])
        check('actual_refund_identity', approved['amount_minor'] == '300'
              and approved['order_id'] == order and approved['user_id'] == fixture['owner_id']
              and approved['actor_id'] == fixture['admin_id'] and approved['ticket_id'] == ticket_id
              and approved['reason'] == reason and approved['entry_id'] > 0)
        fact = admin.get_by_label('已确认退款事实', exact=True)
        expect(fact).to_contain_text('CNY 3.00 已退入站内余额')
        expect(fact).to_contain_text(f'余额流水 #{approved["entry_id"]}')
        screenshot(admin, '03-admin-approved-refund.png')
        stage(admin, 'approved', 'approval_checked')

        result['stage'] = 'reload_and_explicit_same_key_replays'
        before_reload = len(writes())
        admin.reload()
        fact = admin.get_by_label('已确认退款事实', exact=True)
        expect(fact).to_contain_text(f'退款 #{approved["id"]}')
        admin.get_by_role('button', name='核对退款记录与余额', exact=True).click()
        expect(fact).to_contain_text('CNY 3.00')
        check('known_result_reload_and_get_never_post', len(writes()) == before_reload)
        for replay in range(2):
            repeated = api(admin, 'admin', 'POST', refund_path, good_request)
            data = repeated['body'].get('data') or {}
            check('explicit_same_key_replay_' + str(replay + 1), repeated['status'] == 200
                  and data['id'] == approved['id'] and data['entry_id'] == approved['entry_id']
                  and data['amount_minor'] == '300')
        check('all_replayed_payloads_identical', all(row['payload'] == good_request
              for row in writes('admin', refund_path)[1:]))

        result['stage'] = 'actual_audit_and_support_readback'
        admin.get_by_role('link', name='核对本订单退款审计', exact=True).click()
        expect(admin.get_by_role('heading', name='审计日志', exact=True)).to_be_visible()
        row = admin.locator('tbody tr').filter(has_text='order.consumption_refund')
        expect(row).to_have_count(1)
        row.locator('button[data-audit-detail-trigger]').click()
        audit_output = admin.locator('.output-block').filter(has=admin.get_by_text('脱敏详情', exact=True)).locator('pre')
        expect(audit_output).to_contain_text(f'ticket:{ticket_id}')
        expect(audit_output).to_contain_text('amount:300')
        screenshot(admin, '04-real-refund-audit.png')
        admin.goto(origin + f'/admin/tickets?ticket={ticket_id}')
        expect(admin.get_by_role('heading', name=subject, exact=True)).to_be_visible()
        expect(admin.get_by_label('关联记录', exact=True)).to_contain_text(str(subscription))
        check('admin_reads_original_owned_ticket_without_reply_write', len(writes(path='/api/v1/tickets')) == 1)

        result['stage'] = 'owner_wallet_and_terminal_rights'
        owner.goto(origin + '/account/wallet?currency=CNY')
        expect(owner.get_by_role('heading', name='账户余额', exact=True)).to_be_visible()
        expect(owner.locator('main')).to_contain_text('可用 CNY 3.00')
        ledger_row = owner.locator('tbody tr').filter(has_text='获批消费退款')
        expect(ledger_row).to_have_count(1)
        expect(ledger_row).to_contain_text('CNY 3.00')
        expect(ledger_row.get_by_role('link', name=f'订单 #{order}', exact=True)).to_be_visible()
        owner.get_by_role('button', name='刷新余额', exact=True).click()
        expect(ledger_row).to_have_count(1)
        owner.reload()
        expect(owner.locator('main')).to_contain_text('可用 CNY 3.00')
        screenshot(owner, '05-owner-wallet-once.png')
        wallet = api(owner, 'owner', 'GET', '/api/v1/account/wallet?currency=CNY')['body']['data']
        check('owner_wallet_once', wallet['balance_minor'] == '300' and wallet['reserved_minor'] == '0'
              and len([entry for entry in wallet['entries'] if entry['kind'] == 'consumption_refund'
                       and entry['source_order_id'] == order]) == 1)
        access = api(owner, 'owner', 'GET', f'/api/v1/account/subscriptions/{subscription}/access')
        check('terminal_access_not_recreated', access['status'] == 200
              and access['body']['data']['configured'] is False
              and not access['body']['data'].get('subscription_url')
              and not access['body']['data'].get('token'))
        current = api(owner, 'owner', 'GET', '/api/v1/subscriptions?paged=true&status=active&limit=25')
        check('terminal_subscription_not_active', current['status'] == 200
              and all(item['id'] != subscription for item in current['body']['data']['items']))
        owner.goto(origin + f'/account/orders/{order}')
        expect(owner.locator('main')).to_contain_text('原订阅已到期')
        check('no_automatic_financial_writes_during_reads', len(writes()) == 8)
        check('exact_refund_post_roles_and_count', len(writes('owner', refund_path)) == 1
              and len(writes('admin', refund_path)) == 4)
        expected_failures = [
            {'role': 'owner', 'path': refund_path, 'status': 403},
            {'role': 'admin', 'path': refund_path, 'status': 400}]
        check('only_expected_permission_and_owner_refusals', failures == expected_failures)
        # The actual administrator layout checks public release metadata. Keep
        # that production behavior visible while aborting it before any network
        # access; it is unrelated to this isolated financial acceptance.
        release_urls = {
            'https://api.github.com/repos/zerodenet/zboard/releases/latest',
            'https://api.github.com/repos/zerodenet/zboard/releases?per_page=100'}
        check('external_release_requests_blocked_before_network', all(
            row['role'] == 'admin' and row['method'] == 'GET'
            and row['url'] in release_urls and row['blocked_before_network']
            for row in blocked))
        stage(owner, 'verified', 'verification_checked')
        result['stage'] = 'complete'
        result['passed'] = True
        result['refund_id'] = approved['id']
        result['wallet_entry_id'] = approved['entry_id']
        result['ticket_id'] = ticket_id
        browser.close()
    except Exception:
        result['private_error'] = traceback.format_exc()
        for role, page in pages.items():
            try:
                screenshot(page, 'failed-' + role + '.png')
                write_private('failed-' + role + '-private.html', page.content())
            except Exception:
                pass
        raise
    finally:
        result['api_failures'] = failures
        result['blocked_external_hosts'] = blocked
        result['mutation_counts'] = dict(Counter(row['role'] + ' ' + row['method'] + ' ' + row['path']
                                               for row in writes()))
        write_private('browser-checks.json', json.dumps(result, ensure_ascii=False, indent=2) + '\n')
        if p is not None:
            p.stop()


if __name__ == '__main__':
    main()
