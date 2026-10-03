import type { ConfigObject } from './simple-settings';

export function uniqueTag(preferred: string, tags: string[]): string {
  const base = preferred.trim() || 'proxy';
  let candidate = base;
  let suffix = 2;
  while (tags.includes(candidate)) candidate = `${base}-${suffix++}`;
  return candidate;
}

export function parseShareLink(raw: string): ConfigObject {
  let url: URL;
  try {
    url = new URL(raw.trim());
  } catch {
    throw new Error('Некорректная ссылка подключения.');
  }
  const protocol = url.protocol.replace(':', '');
  if (!['vless', 'trojan', 'http', 'https', 'socks', 'socks5'].includes(protocol))
    throw new Error('Поддерживаются ссылки VLESS, Trojan, HTTP и SOCKS5.');
  const params = url.searchParams;
  const server = url.hostname.replace(/^\[|\]$/g, '');
  if (!server) throw new Error('В ссылке не указан сервер.');
  const server_port = Number(
    url.port || (protocol === 'http' ? 80 : protocol.startsWith('socks') ? 1080 : 443),
  );
  if (!Number.isInteger(server_port) || server_port < 1 || server_port > 65535)
    throw new Error('Порт должен быть от 1 до 65535.');
  let tag: string;
  let username: string;
  let password: string;
  try {
    tag = decodeURIComponent(url.hash.slice(1)) || server;
    username = decodeURIComponent(url.username);
    password = decodeURIComponent(url.password);
  } catch {
    throw new Error('Некорректное кодирование имени или пароля в ссылке.');
  }
  const next: ConfigObject = {
    type: protocol === 'https' ? 'http' : protocol === 'socks5' ? 'socks' : protocol,
    tag,
    server,
    server_port,
  };
  if (['http', 'https', 'socks', 'socks5'].includes(protocol)) {
    if (username) next.username = username;
    if (password) next.password = password;
    if (protocol === 'https') next.tls = { enabled: true, server_name: server };
    return next;
  }
  if (!username)
    throw new Error(protocol === 'vless' ? 'Не указан UUID.' : 'Не указан пароль Trojan.');
  if (protocol === 'vless') {
    if (params.get('encryption') && params.get('encryption') !== 'none')
      throw new Error(
        'Это шифрование VLESS не поддерживается текущим sing-box. Используйте Xray или другую ссылку.',
      );
    next.uuid = username;
    if (params.get('flow')) next.flow = params.get('flow');
  } else next.password = password ? `${username}:${password}` : username;
  const security = params.get('security') || (protocol === 'trojan' ? 'tls' : 'none');
  if (!['none', 'tls', 'reality'].includes(security))
    throw new Error(`Неподдерживаемый режим безопасности: ${security}.`);
  if (security !== 'none') {
    const tls: ConfigObject = { enabled: true, server_name: params.get('sni') || server };
    if (params.get('alpn')) tls.alpn = params.get('alpn')!.split(',').filter(Boolean);
    if (['1', 'true'].includes(params.get('insecure') || params.get('allowInsecure') || ''))
      tls.insecure = true;
    if (params.get('fp')) tls.utls = { enabled: true, fingerprint: params.get('fp') };
    if (security === 'reality') {
      if (!params.get('pbk')) throw new Error('В ссылке Reality отсутствует публичный ключ.');
      tls.reality = {
        enabled: true,
        public_key: params.get('pbk'),
        short_id: params.get('sid') || '',
      };
    }
    next.tls = tls;
  } else if (params.get('pbk') || params.get('sid')) {
    throw new Error('Для параметров Reality требуется security=reality.');
  }
  const network = params.get('type') || 'tcp';
  if (!['tcp', 'ws', 'http', 'grpc', 'httpupgrade', 'quic'].includes(network))
    throw new Error(
      `Транспорт ${network} не поддерживается этим импортом sing-box. Используйте совместимую ссылку или Xray.`,
    );
  if (network === 'tcp' && params.get('headerType') && params.get('headerType') !== 'none')
    throw new Error('TCP-маскировка не поддерживается этим импортом. Настройте транспорт вручную.');
  if (network !== 'tcp') {
    const transport: ConfigObject = { type: network };
    const host = params.get('host');
    if (['ws', 'http', 'httpupgrade'].includes(network) && params.get('path'))
      transport.path = params.get('path');
    if (network === 'http' && host) transport.host = host.split(',').filter(Boolean);
    if (network === 'ws' && host) transport.headers = { Host: host };
    if (network === 'httpupgrade' && host) transport.host = host;
    if (network === 'grpc' && params.get('serviceName'))
      transport.service_name = params.get('serviceName');
    next.transport = transport;
  }
  return next;
}
