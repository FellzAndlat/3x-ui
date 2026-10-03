export type ConfigObject = Record<string, unknown>;

export function object(value: unknown): ConfigObject {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as ConfigObject)
    : {};
}

export function records(value: unknown): ConfigObject[] {
  return Array.isArray(value) ? value.map(object) : [];
}

export const DNS_PRESETS = {
  system: { type: 'local' },
  cloudflare: {
    type: 'https',
    server: '1.1.1.1',
    server_port: 443,
    path: '/dns-query',
    tls: { enabled: true, server_name: 'cloudflare-dns.com' },
  },
  quad9: {
    type: 'https',
    server: '9.9.9.9',
    server_port: 443,
    path: '/dns-query',
    tls: { enabled: true, server_name: 'dns.quad9.net' },
  },
} satisfies Record<string, ConfigObject>;

export type DnsPreset = keyof typeof DNS_PRESETS;

function stableJson(value: unknown): string {
  if (Array.isArray(value)) return '[' + value.map(stableJson).join(',') + ']';
  if (value !== null && typeof value === 'object') {
    return (
      '{' +
      Object.keys(value)
        .sort()
        .map((key) => JSON.stringify(key) + ':' + stableJson(object(value)[key]))
        .join(',') +
      '}'
    );
  }
  return JSON.stringify(value) ?? 'null';
}

function matchesPreset(server: ConfigObject, preset: ConfigObject): boolean {
  const { tag: _tag, ...settings } = server;
  return stableJson(settings) === stableJson(preset);
}

export function currentDnsPreset(config: ConfigObject): DnsPreset | 'custom' {
  const dns = object(config.dns);
  const servers = records(dns.servers);
  const selected =
    servers.find((server) => server.tag === dns.final) ?? (dns.final ? undefined : servers[0]);
  return (
    (Object.keys(DNS_PRESETS) as DnsPreset[]).find(
      (key) => selected && matchesPreset(selected, DNS_PRESETS[key]),
    ) ?? 'custom'
  );
}

export function applyDnsPreset(config: ConfigObject, key: DnsPreset): ConfigObject {
  const dns = object(config.dns);
  const servers = records(dns.servers);
  const preset = DNS_PRESETS[key];
  const existing = servers.find(
    (server) => typeof server.tag === 'string' && server.tag && matchesPreset(server, preset),
  );
  let tag = existing?.tag as string | undefined;
  if (!tag) {
    const base = `panel-dns-${key}`;
    tag = base;
    let suffix = 2;
    while (servers.some((server) => server.tag === tag)) tag = `${base}-${suffix++}`;
  }
  const route = object(config.route);
  const resolver = route.default_domain_resolver;
  const previousTag = dns.final || servers[0]?.tag;
  const resolverObject = object(resolver);
  const nextResolver =
    !resolver || resolver === previousTag
      ? tag
      : resolverObject.server && resolverObject.server === previousTag
        ? { ...resolverObject, server: tag }
        : resolver;
  return {
    ...config,
    dns: { ...dns, servers: existing ? servers : [...servers, { ...preset, tag }], final: tag },
    route: {
      ...route,
      default_domain_resolver: nextResolver,
    },
  };
}

export function setDefaultOutbound(config: ConfigObject, tag: string): ConfigObject {
  const route = { ...object(config.route) };
  if (tag) route.final = tag;
  else delete route.final;
  return { ...config, route };
}

export function outboundDefaults(type: string, tag: string, group: string[] = []): ConfigObject {
  const base = { type, tag };
  if (type === 'selector') return { ...base, outbounds: group };
  if (type === 'urltest')
    return {
      ...base,
      outbounds: group,
      url: 'https://www.gstatic.com/generate_204',
      interval: '3m',
      tolerance: 50,
    };
  if (['direct', 'block', 'dns'].includes(type)) return base;
  return {
    ...base,
    server_port: type === 'ssh' ? 22 : type === 'socks' ? 1080 : 443,
    ...(type === 'vmess' ? { security: 'auto' } : {}),
    ...(type === 'shadowsocks' ? { method: 'aes-128-gcm' } : {}),
    ...(['trojan', 'hysteria2', 'tuic'].includes(type) ? { tls: { enabled: true } } : {}),
  };
}

export function outboundErrors(value: ConfigObject, otherTags: string[]): string[] {
  const errors: string[] = [];
  const tag = typeof value.tag === 'string' ? value.tag.trim() : '';
  const type = typeof value.type === 'string' ? value.type : '';
  if (!tag) errors.push('Укажите название подключения.');
  else if (otherTags.includes(tag))
    errors.push('Это название уже используется другим подключением или туннелем.');
  if (!type) errors.push('Выберите протокол.');
  if (['dns', 'tun', 'redirect', 'tproxy', 'wireguard'].includes(type))
    errors.push('Этот тип не поддерживается как исходящее подключение текущим sing-box.');
  if (
    [
      'vless',
      'vmess',
      'trojan',
      'shadowsocks',
      'http',
      'socks',
      'hysteria2',
      'tuic',
      'shadowtls',
      'ssh',
    ].includes(type)
  ) {
    if (typeof value.server !== 'string' || !value.server.trim()) errors.push('Укажите сервер.');
    if (
      !Number.isInteger(value.server_port) ||
      Number(value.server_port) < 1 ||
      Number(value.server_port) > 65535
    )
      errors.push('Порт должен быть целым числом от 1 до 65535.');
  }
  if (
    ['vless', 'vmess', 'tuic'].includes(type) &&
    (typeof value.uuid !== 'string' ||
      !/^[\da-f]{8}(-[\da-f]{4}){3}-[\da-f]{12}$/i.test(value.uuid))
  )
    errors.push('Укажите корректный UUID.');
  if (['trojan', 'shadowsocks', 'hysteria2', 'tuic'].includes(type) && !value.password)
    errors.push('Укажите пароль.');
  if (type === 'shadowsocks' && !value.method) errors.push('Выберите метод шифрования.');
  if (['hysteria2', 'tuic', 'shadowtls'].includes(type) && object(value.tls).enabled !== true)
    errors.push('Для этого протокола требуется TLS.');
  if (['selector', 'urltest'].includes(type)) {
    const members = Array.isArray(value.outbounds) ? value.outbounds : [];
    if (!members.length) errors.push('Выберите подключения для группы.');
    if (members.some((member) => member === tag || !otherTags.includes(String(member))))
      errors.push(
        'Группа должна ссылаться на существующие подключения и не может ссылаться на себя.',
      );
  }
  return errors;
}
