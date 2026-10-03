import { Alert, Button, Card, Col, Row, Select, Space, Switch, Tag } from 'antd';
import { DatabaseOutlined, ExportOutlined, SafetyOutlined, SwapOutlined } from '@ant-design/icons';
import {
  applyDnsPreset,
  currentDnsPreset,
  object,
  records,
  setDefaultOutbound,
  setDnsCache,
  setLogLevel,
} from './simple-settings';
import type { ConfigObject, DnsPreset } from './simple-settings';

type Props = {
  config: ConfigObject;
  onChange: (config: ConfigObject) => void;
  section?: 'basic' | 'dns' | 'routing';
  onNavigate: (section: string) => void;
};

const dnsLabels = {
  system: 'Системный DNS',
  cloudflare: 'Cloudflare · HTTPS',
  quad9: 'Quad9 · HTTPS',
  custom: 'Индивидуальные настройки',
};

export default function SimpleSettings({ config, onChange, section = 'basic', onNavigate }: Props) {
  const dns = object(config.dns);
  const route = object(config.route);
  const log = object(config.log);
  const outbounds = [...records(config.outbounds), ...records(config.endpoints)].filter(
    (item) =>
      typeof item.tag === 'string' && item.tag && !['block', 'dns'].includes(String(item.type)),
  );
  const selectedOutbound = typeof route.final === 'string' ? route.final : '';
  const customOutbound =
    selectedOutbound && !outbounds.some((item) => item.tag === selectedOutbound);
  const preset = currentDnsPreset(config);
  const fallback = records(config.outbounds)[0];
  const effectiveOutbound = selectedOutbound || String(fallback?.tag || 'Не задан');
  const logLevel = log.disabled === true ? 'off' : String(log.level || 'info');
  const logOptions = [
    { value: 'warn', label: 'Предупреждения и ошибки — меньше записей' },
    { value: 'info', label: 'Обычный — события и ошибки' },
    { value: 'debug', label: 'Отладка — для поиска проблем' },
    { value: 'off', label: 'Отключён' },
  ];

  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      {section === 'basic' && (
        <>
          <Card className="singbox-overview">
            <Tag color="blue">sing-box</Tag>
            <h1 className="singbox-overview-title">Быстрая настройка</h1>
            <p className="singbox-overview-description">
              DNS, выход в интернет и журнал — основные настройки сервера. Панель автоматически
              формирует входящие подключения, учёт трафика и служебные API.
            </p>
            <Row gutter={[12, 12]} className="singbox-summary-grid">
              {[
                {
                  title: 'DNS',
                  value: dnsLabels[preset],
                  detail: dns.disable_cache === true ? 'Кэш отключён' : 'Кэш включён',
                  path: 'dns',
                  icon: <DatabaseOutlined />,
                },
                {
                  title: 'Выход в интернет',
                  value: effectiveOutbound,
                  detail: `Правил: ${records(route.rules).length}`,
                  path: 'routing',
                  icon: <SwapOutlined />,
                },
                {
                  title: 'Исходящие подключения',
                  value: String(outbounds.length),
                  detail: 'Серверы и группы подключений',
                  path: 'outbound',
                  icon: <ExportOutlined />,
                },
              ].map((item) => (
                <Col xs={24} md={8} key={item.path}>
                  <button
                    className="singbox-summary"
                    aria-label={`Настроить: ${item.title}`}
                    onClick={() => onNavigate(`/singbox#${item.path}`)}
                  >
                    <span className="singbox-summary-label">
                      {item.icon} {item.title}
                    </span>
                    <strong className="singbox-summary-value">{item.value}</strong>
                    <span className="singbox-summary-detail">{item.detail}</span>
                    <span className="singbox-summary-action">Настроить →</span>
                  </button>
                </Col>
              ))}
            </Row>
          </Card>
          <Card title="Журнал событий">
            <p className="singbox-description">
              Обычный режим подходит для повседневной работы. Отладку включайте на время поиска
              ошибки.
            </p>
            <label className="singbox-field-label" htmlFor="singbox-simple-log">
              Подробность журнала
            </label>
            <Select
              id="singbox-simple-log"
              style={{ width: '100%' }}
              value={logLevel}
              options={[
                ...logOptions,
                ...(!logOptions.some((option) => option.value === logLevel)
                  ? [{ value: logLevel, label: `Текущий уровень: ${logLevel}` }]
                  : []),
              ]}
              onChange={(level) => onChange(setLogLevel(config, level))}
            />
            <p className="singbox-description">
              Файл вывода и дополнительные параметры доступны в расширенном режиме.
            </p>
          </Card>
          <Card title="Связанные разделы" size="small">
            <Space wrap>
              <Button onClick={() => onNavigate('/inbounds')}>Входящие подключения</Button>
              <Button onClick={() => onNavigate('/settings#subscription')}>Подписка</Button>
              <Button icon={<SafetyOutlined />} onClick={() => onNavigate('/singbox#adblock')}>
                Блокировка рекламы
              </Button>
            </Space>
          </Card>
        </>
      )}

      {section === 'dns' && (
        <Card title="Определение адресов сайтов (DNS)">
          <p className="singbox-description">
            Выберите готовый сервис: адрес, порт и проверка TLS настроятся автоматически. Системный
            DNS использует настройки вашего сервера.
          </p>
          <Row gutter={[16, 20]}>
            <Col xs={24}>
              <label className="singbox-field-label" htmlFor="singbox-simple-dns">
                DNS-сервис
              </label>
              <Select
                id="singbox-simple-dns"
                style={{ width: '100%' }}
                value={preset}
                options={[
                  { value: 'system', label: 'Системный — настройки сервера' },
                  { value: 'cloudflare', label: 'Cloudflare — зашифрованный DNS' },
                  {
                    value: 'quad9',
                    label: 'Quad9 — зашифрованный DNS с защитой от вредоносных доменов',
                  },
                  ...(preset === 'custom'
                    ? [{ value: 'custom', label: 'Ваши текущие настройки', disabled: true }]
                    : []),
                ]}
                onChange={(value: DnsPreset | 'custom') => {
                  if (value !== 'custom') onChange(applyDnsPreset(config, value));
                }}
              />
            </Col>
            <Col xs={24} md={16}>
              <label className="singbox-field-label" htmlFor="singbox-simple-strategy">
                Выбор IP-адреса
              </label>
              <Select
                id="singbox-simple-strategy"
                style={{ width: '100%' }}
                value={String(dns.strategy || '')}
                options={[
                  { value: '', label: 'Автоматически — выбор sing-box' },
                  { value: 'prefer_ipv4', label: 'Сначала IPv4' },
                  { value: 'prefer_ipv6', label: 'Сначала IPv6' },
                  { value: 'ipv4_only', label: 'Только IPv4 — если IPv6 не работает' },
                  { value: 'ipv6_only', label: 'Только IPv6' },
                ]}
                onChange={(strategy) => {
                  const next = { ...dns };
                  if (strategy) next.strategy = strategy;
                  else delete next.strategy;
                  onChange({ ...config, dns: next });
                }}
              />
            </Col>
            <Col xs={24} md={8}>
              <div className="singbox-switch-setting">
                <div>
                  <span>Кэш DNS</span>
                  <p className="singbox-description">Ускоряет повторные запросы</p>
                </div>
                <Switch
                  aria-label="Кэш DNS"
                  checked={dns.disable_cache !== true}
                  onChange={(enabled) => onChange(setDnsCache(config, enabled))}
                />
              </div>
            </Col>
          </Row>
          <p className="singbox-description">
            Готовые HTTPS-сервисы подключаются напрямую с этого сервера. Правила DNS и отдельные
            серверы сохраняются; правила имеют приоритет над выбранным сервисом.
          </p>
          {preset === 'custom' && (
            <Alert
              type="info"
              showIcon
              title="Используется ваша конфигурация DNS"
              description="Она сохранится, пока вы не выберете готовый сервис. Для редактирования включите расширенный режим."
            />
          )}
        </Card>
      )}

      {section === 'routing' && (
        <Card title="Выход по умолчанию">
          <p className="singbox-description">
            Куда отправлять трафик, для которого нет отдельного правила.
          </p>
          <Select
            aria-label="Выход в интернет"
            style={{ width: '100%' }}
            value={selectedOutbound}
            options={[
              {
                value: '',
                label: `Автоматически — ${String(fallback?.tag || 'первое исходящее подключение')}`,
              },
              ...outbounds.map((item) => ({
                value: String(item.tag),
                label:
                  item.type === 'direct'
                    ? `Напрямую с этого сервера (${item.tag})`
                    : `${item.tag} (${item.type})`,
              })),
              ...(customOutbound
                ? [
                    {
                      value: selectedOutbound,
                      label: `Текущий выход: ${selectedOutbound}`,
                      disabled: true,
                    },
                  ]
                : []),
            ]}
            onChange={(tag) => onChange(setDefaultOutbound(config, tag))}
          />
          {!outbounds.length && (
            <Alert
              style={{ marginTop: 16 }}
              type="warning"
              showIcon
              title="Добавьте исходящее подключение"
              description="Для выхода в интернет нужен прямой выход или подключение к другому серверу."
            />
          )}
          <Space wrap style={{ marginTop: 16 }}>
            <Button onClick={() => onNavigate('/singbox#outbound')}>
              Настроить исходящие подключения
            </Button>
            <Button onClick={() => onNavigate('/singbox#adblock')}>Блокировка рекламы</Button>
          </Space>
        </Card>
      )}
    </Space>
  );
}
