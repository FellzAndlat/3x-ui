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
  for (const key of ['ech', 'pcs', 'pinSHA256', 'pqv', 'fm']) {
    if (params.get(key))
      throw new Error(`Параметр ${key} не поддерживается этим импортом sing-box.`);
  }
  if (
    params.get('vcn') &&
    params.get('vcn') !== (params.get('sni') || url.hostname.replace(/^\[|\]$/g, ''))
  )
    throw new Error('Отдельное имя проверки сертификата не поддерживается этим импортом.');
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
    if (protocol === 'https') {
      const tls: ConfigObject = { enabled: true, server_name: params.get('sni') || server };
      if (params.get('alpn')) tls.alpn = params.get('alpn')!.split(',').filter(Boolean);
      if (['1', 'true'].includes(params.get('allowInsecure') || params.get('insecure') || ''))
        tls.insecure = true;
      next.tls = tls;
    }
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
  } else {
    const authority = raw.trim().split('://', 2)[1]?.split(/[/?#]/, 1)[0] ?? '';
    next.password = authority.slice(0, authority.lastIndexOf('@')).includes(':')
      ? `${username}:${password}`
      : username;
  }
  const security = params.get('security') || (protocol === 'trojan' ? 'tls' : 'none');
  if (!['none', 'tls', 'reality'].includes(security))
    throw new Error(`Неподдерживаемый режим безопасности: ${security}.`);
  if (security !== 'none') {
    const tls: ConfigObject = { enabled: true, server_name: params.get('sni') || server };
    if (params.get('alpn')) tls.alpn = params.get('alpn')!.split(',').filter(Boolean);
    if (['1', 'true'].includes(params.get('allowInsecure') || params.get('insecure') || ''))
      tls.insecure = true;
    const fingerprint = params.get('fp') || (security === 'reality' ? 'chrome' : '');
    if (fingerprint && fingerprint !== 'unsafe') tls.utls = { enabled: true, fingerprint };
    if (security === 'reality' && fingerprint === 'unsafe')
      throw new Error('Reality требует uTLS fingerprint.');
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
  if (!['tcp', 'ws', 'http', 'grpc', 'httpupgrade'].includes(network))
    throw new Error(
      `Транспорт ${network} не поддерживается этим импортом sing-box. Используйте совместимую ссылку или Xray.`,
    );
  if (network === 'tcp' && params.get('headerType') && params.get('headerType') !== 'none')
    throw new Error('TCP-маскировка не поддерживается этим импортом. Настройте транспорт вручную.');
  if (network === 'grpc' && (params.get('mode') === 'multi' || params.get('authority')))
    throw new Error('gRPC multiMode и authority не поддерживаются этим импортом sing-box.');
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
