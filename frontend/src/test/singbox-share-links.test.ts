import { describe, expect, it } from 'vitest';
import { parseShareLink, uniqueTag } from '@/pages/singbox/share-links';
import { outboundErrors } from '@/pages/singbox/simple-settings';

const uuid = '12345678-1234-1234-1234-123456789abc';
describe('sing-box share link import', () => {
  it('enables TLS for a standard Trojan link and decodes the complete password', () => {
    expect(parseShareLink('trojan://hello%3Aworld@proxy.test#Test')).toMatchObject({
      password: 'hello:world',
      tls: { enabled: true, server_name: 'proxy.test' },
    });
    expect(parseShareLink('trojan://hello:world@proxy.test')).toHaveProperty(
      'password',
      'hello:world',
    );
  });
  it.each([
    ['http', 80],
    ['https', 443],
    ['socks', 1080],
    ['socks5', 1080],
  ])('uses the default %s port', (scheme, port) => {
    expect(parseShareLink(`${scheme}://user:pass@proxy.test`)).toHaveProperty('server_port', port);
  });
  it('preserves explicit ports and IPv6 addresses', () => {
    expect(parseShareLink('socks5://[2001:db8::1]:9000')).toMatchObject({
      server: '2001:db8::1',
      server_port: 9000,
    });
  });
  it('imports Trojan WebSocket and TLS options', () => {
    expect(
      parseShareLink(
        'trojan://pass@proxy.test?type=ws&path=%2Fsocket&host=cdn.test&sni=tls.test&alpn=h2&fp=chrome',
      ),
    ).toMatchObject({
      transport: { type: 'ws', path: '/socket', headers: { Host: 'cdn.test' } },
      tls: {
        enabled: true,
        server_name: 'tls.test',
        alpn: ['h2'],
        utls: { enabled: true, fingerprint: 'chrome' },
      },
    });
  });
  it('uses the native HTTP host list and gRPC service name without leaking HTTP fields', () => {
    expect(
      parseShareLink(`vless://${uuid}@proxy.test?type=http&host=a.test,b.test`),
    ).toHaveProperty('transport.host', ['a.test', 'b.test']);
    expect(
      parseShareLink(`vless://${uuid}@proxy.test?type=grpc&serviceName=service&path=/bad&host=bad`),
    ).toHaveProperty('transport', { type: 'grpc', service_name: 'service' });
  });
  it('imports Reality even when the optional short ID is empty', () => {
    expect(
      parseShareLink(`vless://${uuid}@proxy.test?security=reality&pbk=key&sid=`),
    ).toHaveProperty('tls.reality', { enabled: true, public_key: 'key', short_id: '' });
  });
  it.each([
    'type=xhttp',
    'type=kcp',
    'encryption=mlkem768x25519plus.native',
    'security=reality',
    'type=tcp&headerType=http',
  ])('rejects unsupported or incomplete settings: %s', (query) => {
    expect(() => parseShareLink(`vless://${uuid}@proxy.test?${query}`)).toThrow();
  });
  it('avoids tag collisions with imported names and endpoints', () => {
    expect(uniqueTag('proxy', ['proxy', 'proxy-2', 'proxy-3'])).toBe('proxy-4');
  });
});

describe('outbound validation', () => {
  it('checks TUIC UUID, password, port and TLS before adding a connection', () => {
    expect(
      outboundErrors(
        {
          type: 'tuic',
          tag: 'test',
          server: 'proxy.test',
          server_port: 0,
          tls: { enabled: false },
        },
        [],
      ),
    ).toHaveLength(4);
    expect(
      outboundErrors(
        {
          type: 'tuic',
          tag: 'test',
          server: 'proxy.test',
          server_port: 443,
          uuid,
          password: 'pass',
          tls: { enabled: true },
        },
        [],
      ),
    ).toEqual([]);
  });
  it('rejects duplicate tags and empty or self-referencing groups', () => {
    expect(outboundErrors({ type: 'direct', tag: 'proxy' }, ['proxy'])).toHaveLength(1);
    expect(
      outboundErrors({ type: 'selector', tag: 'group', outbounds: [] }, ['proxy']),
    ).toHaveLength(1);
    expect(
      outboundErrors({ type: 'selector', tag: 'group', outbounds: ['group'] }, ['proxy']),
    ).toHaveLength(1);
  });
});
