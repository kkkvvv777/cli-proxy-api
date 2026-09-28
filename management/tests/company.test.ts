import { afterEach, describe, expect, spyOn, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { apiClient } from '../src/services/api/client';
import { companyApi, isCompanyMode } from '../src/services/api/company';
import en from '../src/i18n/locales/en.json';
import zh from '../src/i18n/locales/zh-CN.json';
import tw from '../src/i18n/locales/zh-TW.json';
import ru from '../src/i18n/locales/ru.json';

const restore: Array<() => void> = [];
afterEach(() => {
  restore.splice(0).forEach((callback) => callback());
});

describe('company management integration', () => {
  test('requires an explicit company capability from the authenticated config', () => {
    expect(isCompanyMode(null)).toBe(false);
    expect(isCompanyMode({ raw: { 'company-gateway': { enabled: false } } })).toBe(false);
    expect(isCompanyMode({ raw: { 'company-gateway': { enabled: 'true' } } })).toBe(false);
    expect(isCompanyMode({ raw: { 'company-gateway': { enabled: true } } })).toBe(true);
  });
  test('normalizes employees through the shared authenticated client', async () => {
    const get = spyOn(apiClient, 'get').mockResolvedValue({
      users: [{ id: 'e001', name: 'Employee', key_prefix: 'cpa_prefix', enabled: true }],
    });
    restore.push(() => get.mockRestore());
    expect(await companyApi.list()).toEqual([
      { id: 'e001', name: 'Employee', keyPrefix: 'cpa_prefix', enabled: true },
    ]);
    expect(get.mock.calls[0][0]).toBe('/company/users');
  });
  test('create and rotate return the one-time key', async () => {
    const post = spyOn(apiClient, 'post').mockResolvedValue({
      user: { id: 'e001', name: 'Employee', key_prefix: 'prefix', enabled: true },
      api_key: 'fixture-key',
    });
    restore.push(() => post.mockRestore());
    expect((await companyApi.create('e001', 'Employee')).apiKey).toBe('fixture-key');
    expect((await companyApi.rotate('e001')).apiKey).toBe('fixture-key');
    expect(post.mock.calls[1][0]).toBe('/company/users/e001/rotate');
  });
  test('audit export retains models and excludes diagnostic bodies', async () => {
    const get = spyOn(apiClient, 'get').mockResolvedValue({
      healthy: true,
      entries: [
        {
          kind: 'attempt',
          timestamp: '2026-09-22T00:00:00Z',
          user_id: 'e001',
          user_name: 'Employee',
          requested_model: 'alias',
          model: 'upstream',
          response_model: 'actual',
          provider: 'mock',
          status_code: 200,
          failed: true,
          stream: true,
          input_tokens: 3,
          output_tokens: 2,
          total_tokens: 5,
          latency_ms: 100,
          request_id: 'req-1',
          body: 'must-not-be-exported',
        },
      ],
    });
    restore.push(() => get.mockRestore());
    const data = await companyApi.audit(' e001 ', 500);
    expect(data.entries[0]).toMatchObject({
      requestedModel: 'alias',
      model: 'actual',
      failed: true,
      stream: true,
      userId: 'e001',
      requestId: 'req-1',
      totalTokens: 5,
    });
    expect(JSON.stringify(data)).not.toContain('must-not-be-exported');
    expect(get.mock.calls[0][1]?.params).toEqual({ user_id: 'e001', limit: 500 });
  });
  test('all existing languages contain the same company labels', () => {
    const keys = Object.keys(en.company).sort();
    for (const locale of [zh, tw, ru]) expect(Object.keys(locale.company).sort()).toEqual(keys);
  });
  test('company fields reuse existing config and login without an iframe', () => {
    const root = new URL('../src/', import.meta.url);
    const field = readFileSync(
      new URL('features/config/components/fields/sharedFields.tsx', root),
      'utf8'
    );
    expect(field).toContain('EmployeeKeysEditor');
    expect(field).toContain('ApiKeysCardEditor');
    const routes = readFileSync(new URL('router/MainRoutes.tsx', root), 'utf8');
    expect(routes).toContain("path: '/company/audit'");
    for (const name of ['EmployeeKeysEditor', 'CompanyAuditPage']) {
      const source = readFileSync(new URL(`features/company/${name}.tsx`, root), 'utf8');
      expect(source).not.toContain('<iframe');
      expect(source).not.toContain('localStorage');
      expect(source).not.toContain('sessionStorage');
      expect(source).not.toContain('fetch(');
      expect(source).toContain('useCompanySession');
    }
  });
});
