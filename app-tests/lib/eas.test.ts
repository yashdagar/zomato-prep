import { readFileSync } from 'fs';
import { join } from 'path';
import { apiUrlFor, checkEasConfig } from '@app/eas';

const valid = () => ({
  cli: { version: '>= 16.0.0' },
  build: {
    development: { developmentClient: true, distribution: 'internal', env: { EXPO_PUBLIC_API_URL: 'http://192.168.1.20:8080' } },
    preview: {
      distribution: 'internal',
      android: { buildType: 'apk' },
      env: { EXPO_PUBLIC_API_URL: 'https://api.example.com' },
    },
    production: { autoIncrement: true, env: { EXPO_PUBLIC_API_URL: 'https://api.example.com' } },
  },
});

describe('W7 B5 EAS preview build and API URL', () => {
  test('a valid https URL is returned unchanged for the preview profile', () => {
    expect(apiUrlFor({ EXPO_PUBLIC_API_URL: 'https://api.example.com' }, 'preview')).toBe('https://api.example.com');
  });

  test('trailing slashes and surrounding spaces are removed', () => {
    expect(apiUrlFor({ EXPO_PUBLIC_API_URL: '  https://api.example.com//  ' }, 'production')).toBe('https://api.example.com');
  });

  test('a missing or blank URL throws in every profile', () => {
    for (const profile of ['development', 'preview', 'production'] as const) {
      expect(() => apiUrlFor({}, profile)).toThrow(/EXPO_PUBLIC_API_URL/);
      expect(() => apiUrlFor({ EXPO_PUBLIC_API_URL: '   ' }, profile)).toThrow(/EXPO_PUBLIC_API_URL/);
    }
  });

  test('plain http is rejected for preview and production builds', () => {
    expect(() => apiUrlFor({ EXPO_PUBLIC_API_URL: 'http://api.example.com' }, 'preview')).toThrow(/https/);
    expect(() => apiUrlFor({ EXPO_PUBLIC_API_URL: 'http://api.example.com' }, 'production')).toThrow(/https/);
  });

  test('plain http to a laptop on the LAN is allowed in development', () => {
    expect(apiUrlFor({ EXPO_PUBLIC_API_URL: 'http://192.168.1.20:8080/' }, 'development')).toBe('http://192.168.1.20:8080');
  });

  test('a value that is not a URL is rejected', () => {
    expect(() => apiUrlFor({ EXPO_PUBLIC_API_URL: 'api.example.com' }, 'development')).toThrow();
  });

  test('a URL with embedded credentials is rejected', () => {
    expect(() => apiUrlFor({ EXPO_PUBLIC_API_URL: 'https://admin:hunter2@api.example.com' }, 'preview')).toThrow(/credentials/);
  });

  test('a correct eas.json has no problems', () => {
    expect(checkEasConfig(valid())).toEqual([]);
  });

  test('a config that is not an object is reported', () => {
    expect(checkEasConfig(null).length).toBeGreaterThan(0);
    expect(checkEasConfig('eas').length).toBeGreaterThan(0);
  });

  test('a missing preview profile is reported', () => {
    const cfg = valid() as any;
    delete cfg.build.preview;
    expect(checkEasConfig(cfg).some(p => p.includes('preview'))).toBe(true);
  });

  test('an app bundle instead of an apk is reported', () => {
    const cfg = valid() as any;
    cfg.build.preview.android.buildType = 'app-bundle';
    expect(checkEasConfig(cfg).some(p => p.includes('apk'))).toBe(true);
  });

  test('a preview build that is not internal distribution is reported', () => {
    const cfg = valid() as any;
    cfg.build.preview.distribution = 'store';
    expect(checkEasConfig(cfg).some(p => p.includes('internal'))).toBe(true);
  });

  test('a missing API URL in the preview profile is reported', () => {
    const cfg = valid() as any;
    delete cfg.build.preview.env;
    expect(checkEasConfig(cfg).some(p => p.includes('EXPO_PUBLIC_API_URL'))).toBe(true);
  });

  test('a plain http API URL in the preview profile is reported', () => {
    const cfg = valid() as any;
    cfg.build.preview.env.EXPO_PUBLIC_API_URL = 'http://api.example.com';
    expect(checkEasConfig(cfg).some(p => p.includes('https'))).toBe(true);
  });

  test('a secret-looking variable in any profile env is reported', () => {
    const cfg = valid() as any;
    cfg.build.production.env.EXPO_PUBLIC_JWT_SECRET = 'abc';
    cfg.build.preview.env.EXPO_PUBLIC_ADMIN_TOKEN = 'abc';
    const problems = checkEasConfig(cfg);
    expect(problems.filter(p => p.toLowerCase().includes('secret')).length).toBeGreaterThanOrEqual(2);
  });

  test('the eas.json in the app folder passes every check', () => {
    const raw = readFileSync(join(__dirname, '..', '..', 'app', 'eas.json'), 'utf8');
    expect(checkEasConfig(JSON.parse(raw))).toEqual([]);
  });
});
