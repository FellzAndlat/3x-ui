import { Alert, Button, Card, Col, Row, Select, Space, Switch } from 'antd';
import {
  applyDnsPreset,
  currentDnsPreset,
  object,
  records,
  setDefaultOutbound,
} from './simple-settings';
import type { ConfigObject, DnsPreset } from './simple-settings';

type Props = {
  config: ConfigObject;
  onChange: (config: ConfigObject) => void;
  section?: 'basic' | 'dns' | 'routing';
  onNavigate: (section: string) => void;
};

export default function SimpleSettings({ config, onChange, section = 'basic', onNavigate }: Props) {
  const dns = object(config.dns);
  const route = object(config.route);
  const outbounds = [...records(config.outbounds), ...records(config.endpoints)].filter(
    (item) =>
      typeof item.tag === 'string' && item.tag && !['block', 'dns'].includes(String(item.type)),
  );
  const selectedOutbound = typeof route.final === 'string' ? route.final : '';
  const customOutbound =
    selectedOutbound && !outbounds.some((item) => item.tag === selectedOutbound);
  const preset = currentDnsPreset(config);

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {section === 'basic' && (
        <Card title="Быстрая настройка">
          <p>
            Здесь оставлены только ссылки на основные связанные разделы. DNS, маршрутизация,
            исходящие подключения и служебные параметры настраиваются в собственных вкладках без
            повторения одних и тех же полей.
          </p>
          <p>
            Входящие подключения и пользователи настраиваются в разделе «Входящие», ссылки для
            приложений — в настройках подписки.
          </p>
          <Space wrap>
            <Button onClick={() => onNavigate('/inbounds')}>Входящие подключения</Button>
            <Button onClick={() => onNavigate('/settings#subscription')}>Подписка</Button>
          </Space>
        </Card>
      )}

      {section === 'dns' && (
        <Card title="Определение адресов сайтов (DNS)">
          <p>
            Системный DNS использует настройки сервера. Зашифрованный DNS отправляет запросы
            выбранному провайдеру через HTTPS.
          </p>
          <Row gutter={[16, 16]}>
            <Col xs={24} md={16}>
              <label htmlFor="singbox-simple-dns">DNS-сервис</label>
              <Select
                id="singbox-simple-dns"
                style={{ width: '100%' }}
                value={preset}
                options={[
                  { value: 'system', label: 'Системный — простой вариант' },
                  { value: 'cloudflare', label: 'Cloudflare — зашифрованный DNS' },
                  {
                    value: 'quad9',
                    label: 'Quad9 — зашифрованный DNS с фильтрацией вредоносных доменов',
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
            <Col xs={24} md={8}>
              <Space orientation="vertical">
                <span>Кэш DNS — ускоряет повторные запросы</span>
                <Switch
                  aria-label="Кэш DNS"
                  checked={dns.disable_cache !== true}
                  onChange={(checked) =>
                    onChange({ ...config, dns: { ...dns, disable_cache: !checked } })
                  }
                />
              </Space>
            </Col>
          </Row>
          <p className="singbox-field-hint">
            Правила DNS сохраняются и имеют приоритет над выбранным сервисом. Индивидуальные
            DNS-серверы доступны в расширенном режиме этой же вкладки.
          </p>
        </Card>
      )}

      {section === 'routing' && (
        <Card title="Выход по умолчанию">
          <p>Выберите, куда отправлять трафик, для которого нет отдельного правила.</p>
          <Select
            aria-label="Выход в интернет"
            style={{ width: '100%' }}
            value={selectedOutbound}
            options={[
              { value: '', label: 'Автоматически — первое исходящее подключение' },
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
          <Space wrap style={{ marginTop: 16 }}>
            <Button onClick={() => onNavigate('/singbox#outbound')}>
              Настроить исходящие подключения
            </Button>
            <Button onClick={() => onNavigate('/singbox#adblock')}>Блокировка рекламы</Button>
          </Space>
        </Card>
      )}

      <Alert
        type="info"
        showIcon
        title="Автоматическое управление"
        description="Панель формирует входящие подключения, учёт трафика и служебные API. Дополнительные параметры находятся в отдельных вкладках расширенного режима."
      />
    </Space>
  );
}
