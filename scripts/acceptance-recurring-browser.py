#!/usr/bin/env python3
"""Opt-in loopback Chromium driver for TestAutomaticRecurringChromiumAcceptance.

All UI/API data comes from the real built app and Go HTTP handlers. Stage files
coordinate the isolated fixture worker/provider; no API response is intercepted
or fabricated. Raw screenshots/authentication remain in a private evidence dir.
"""
import argparse
import json
import os
from pathlib import Path
import stat
import time
import traceback
from urllib.parse import urlparse

from playwright.sync_api import sync_playwright, expect


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--fixture', type=Path, required=True)
    parser.add_argument('--chromium', default='/usr/bin/chromium', help='existing browser executable; sandbox remains enabled')
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
        if entry.name.endswith(('.request', '.done')) or entry.name in ('browser-checks.json', 'financial-checks.json'):
            raise RuntimeError('Fresh evidence directory required; existing phase or result file')
    origin = fixture['origin']
    parsed_origin = urlparse(origin)
    if parsed_origin.scheme != 'http' or parsed_origin.hostname != '127.0.0.1':
        raise RuntimeError('Only isolated loopback HTTP is allowed')
    result = {'passed': False, 'checks': {}, 'stage': 'launch', 'limits': [
        'isolated SQLite/selected real handlers; not complete router acceptance',
        'signed native test fixture and real trusted wallet facts; not production',
        'only scheduler/target dates compressed; no passage-of-month claim',
        'no API mocks, no external websites, no capacity claim']}
    puts, failures, blocked = [], [], []
    page = None

    def check(name, condition):
        result['checks'][name] = bool(condition)
        if not condition:
            raise AssertionError(name)

    def stage(name, response):
        (evidence / (name + '.request')).write_text('ready')
        deadline = time.monotonic() + 60
        while not (evidence / (response + '.done')).exists():
            if time.monotonic() >= deadline:
                raise TimeoutError('Go fixture phase: ' + response)
            page.wait_for_timeout(100)

    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(executable_path=args.chromium, headless=True,
                chromium_sandbox=True, args=['--disable-dev-shm-usage', '--disable-background-networking',
                      '--disable-component-update', '--no-first-run'])
            result['chromium_version'] = browser.version
            context = browser.new_context(viewport={'width': 1440, 'height': 1000}, service_workers='block')

            def route_request(route):
                url = urlparse(route.request.url)
                if f'{url.scheme}://{url.netloc}' != origin:
                    blocked.append(url.hostname)
                    route.abort()
                    return
                route.continue_()

            context.route('**/*', route_request)
            page = context.new_page()
            page.set_default_timeout(20000)
            api_path = f"/api/v1/subscriptions/{fixture['subscription_id']}/automatic-renewal"

            def request_seen(request):
                if request.method == 'PUT' and urlparse(request.url).path == api_path:
                    puts.append(request.post_data_json)

            def response_seen(response):
                if '/api/' in response.url and response.status >= 400:
                    failures.append({'path': urlparse(response.url).path, 'status': response.status})

            page.on('request', request_seen)
            page.on('response', response_seen)
            result['stage'] = 'real_login'
            page.goto(origin + '/login?redirect=/account/subscription')
            page.locator('#login-email').fill(fixture['email'])
            page.locator('#login-password').fill(fixture['password'])
            with page.expect_response(lambda response: urlparse(response.url).path == '/api/v1/auth/login') as login:
                page.get_by_role('button', name='登录', exact=True).click()
            check('real_http_login', login.value.status == 200)
            page.wait_for_url('**/account/subscription')
            page.get_by_role('button', name='余额自动续费', exact=True).click()
            panel = page.locator('.ui-section').filter(has=page.get_by_role('heading', name=f"订阅 #{fixture['subscription_id']} 的余额自动续费", exact=True))
            enable = panel.get_by_role('button', name='确认此规格并开启余额自动续费', exact=True)
            disable = panel.get_by_role('button', name='关闭余额自动续费', exact=True)
            refresh = panel.get_by_role('button', name='核对当前状态', exact=True)
            result['stage'] = 'offer'
            expect(panel).to_contain_text('基础续费价格为 CNY 120.00')
            expect(panel).to_contain_text('本次核对优惠 CNY 20.00')
            expect(panel).to_contain_text('每次仅从可用站内余额扣除 CNY 100.00')
            expect(panel).to_contain_text('提供方暂不可用时不会按基础价扣款')
            check('120_base_20_discount_100_debit_visible', True)
            check('defaults_disabled_without_automatic_put', len(puts) == 0 and disable.count() == 0)
            page.screenshot(path=str(evidence / '01-confirm-offer.png'), full_page=True)

            def save(button, enabled):
                previous = len(puts)
                with page.expect_response(lambda response: urlparse(response.url).path == api_path and response.request.method == 'PUT') as response:
                    button.click()
                check('real_put_success_' + str(previous + 1), response.value.status == 200)
                body = response.value.json()['data']
                check('explicit_put_' + str(previous + 1), len(puts) == previous + 1 and body['enabled'] is enabled)
                check('only_consent_references_' + str(previous + 1), set(puts[-1]) == {'enabled', 'plan_sku_id', 'quote_fingerprint', 'expected_revision'})
                return body

            result['stage'] = 'enable'
            save(enable, True)
            expect(disable).to_be_visible()
            page.wait_for_timeout(200)
            check('one_explicit_enable_no_replay', len(puts) == 1)
            stage('enabled', 'renewed')
            result['stage'] = 'real_worker_debit'
            refresh.click()
            expect(panel).to_contain_text('最近订单 #')
            expect(disable).to_be_visible()
            check('worker_order_visible_without_consent_write', len(puts) == 1)
            page.screenshot(path=str(evidence / '02-real-renewed.png'), full_page=True)
            save(disable, False)
            expect(disable).to_have_count(0)
            expect(panel).to_contain_text('已关闭')
            refresh.click()
            expect(enable).to_be_enabled()
            save(enable, True)
            expect(disable).to_be_visible()
            stage('reenabled', 'unavailable')
            result['stage'] = 'provider_unavailable'
            refresh.click()
            expect(disable).to_be_enabled()
            expect(enable).to_have_count(0)
            expect(panel).to_contain_text('优惠')
            check('unavailable_keeps_withdrawable_consent', len(puts) == 3)
            page.screenshot(path=str(evidence / '03-provider-unavailable.png'), full_page=True)
            save(disable, False)
            expect(panel).to_contain_text('已关闭')
            expect(disable).to_have_count(0)
            stage('withdrawn', 'withdrawn')
            page.wait_for_timeout(200)
            check('four_explicit_mutations_no_automatic_replay', len(puts) == 4 and [row['enabled'] for row in puts] == [True, False, True, False])
            check('no_api_failures_or_mock_fallbacks', failures == [])
            check('no_external_requests', blocked == [])
            page.screenshot(path=str(evidence / '04-withdrawn.png'), full_page=True)
            result['stage'] = 'complete'
            result['passed'] = True
            browser.close()
    except Exception:
        result['private_error'] = traceback.format_exc()
        if page is not None:
            try:
                page.screenshot(path=str(evidence / 'failed-stage.png'), full_page=True)
                (evidence / 'failed-page-private.html').write_text(page.content())
            except Exception:
                pass
        raise
    finally:
        result['api_failures'] = failures
        result['blocked_external_hosts'] = blocked
        result['automatic_renewal_put_count'] = len(puts)
        (evidence / 'browser-checks.json').write_text(json.dumps(result, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
