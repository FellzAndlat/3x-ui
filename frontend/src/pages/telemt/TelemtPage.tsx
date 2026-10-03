import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Collapse,
  ConfigProvider,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Layout,
  Modal,
  Popconfirm,
  Row,
  Space,
  Switch,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  ApiOutlined,
  CheckCircleOutlined,
  CopyOutlined,
  DeleteOutlined,
  DownloadOutlined,
  LinkOutlined,
  PlusOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  StopOutlined,
  SyncOutlined,
} from '@ant-design/icons';
import { HttpUtil, RandomUtil } from '@/utils';
import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import './TelemtPage.css';

type WebProxyStatus = {
  enabled: boolean;
  supported: boolean;
  domain: string;
  defaultDomain: string;
  link: string;
  nginxInstalled: boolean;
  nginxActive: boolean;
  certificateReady: boolean;
  certificateFile: string;
  listenPort: number;
  port443Available: boolean;
  port443Owner: string;
  error: string;
};

type Status = {
  installed: boolean;
  active: boolean;
  enabled: boolean;
  configured: boolean;
  version: string;
  latestVersion: string;
  updateAvailable: boolean;
  mekoEnabled: boolean;
  webProxy: WebProxyStatus;
};

type Config = {
  enabled: boolean;
  port: number;
  secret: string;
  ipv4: boolean;
  ipv6: boolean;
  fastMode: boolean;
  classic: boolean;
  secure: boolean;
  tls: boolean;
  sni: string;
  upstreamType: string;
};

type Proxy = {
  name: string;
  secret: string;
  host: string;
  port: number;
  tls: boolean;
  link: string;
};

const defaults: Config = {
  enabled: false,
  port: 8443,
  secret: '',
  ipv4: true,
  ipv6: true,
  fastMode: true,
  classic: false,
  secure: false,
  tls: true,
  sni: 'petrovich.ru',
  upstreamType: 'direct',
};

const jsonOptions = { headers: { 'Content-Type': 'application/json' } };

type ServiceAction = 'start' | 'stop' | 'restart' | 'enable' | 'disable' | 'update';

export default function TelemtPage() {
  const { antdThemeConfig } = useTheme();
  const [form] = Form.useForm<Config>();
  const configuredPort = Form.useWatch('port', form);
  const [status, setStatus] = useState<Status>({
    installed: false,
    active: false,
    enabled: false,
    configured: false,
    version: '',
    latestVersion: '',
    updateAvailable: false,
    mekoEnabled: false,
    webProxy: {
      enabled: false,
      supported: false,
      domain: '',
      defaultDomain: '',
      link: '',
      nginxInstalled: false,
      nginxActive: false,
      certificateReady: false,
      certificateFile: '',
      listenPort: 15080,
      port443Available: true,
      port443Owner: '',
      error: '',
    },
  });
  const [proxies, setProxies] = useState<Proxy[]>([]);
  const [refreshing, setRefreshing] = useState(false);
  const [serviceAction, setServiceAction] = useState<ServiceAction | null>(null);
  const [configSaving, setConfigSaving] = useState(false);
  const [proxyCreating, setProxyCreating] = useState(false);
  const [proxyDeleting, setProxyDeleting] = useState<string | null>(null);
  const [webProxyLoading, setWebProxyLoading] = useState(false);
  const [webProxyOpen, setWebProxyOpen] = useState(false);
  const [webProxyDomain, setWebProxyDomain] = useState('');

  const loadStatus = useCallback(async () => {
    const r = await HttpUtil.get<Status>('/panel/api/telemt/status');
    if (r?.success && r.obj) setStatus(r.obj);
  }, []);

  const loadConfig = useCallback(async () => {
    const r = await HttpUtil.get<Config>('/panel/api/telemt/config');
    if (r?.success && r.obj) form.setFieldsValue({ ...defaults, ...r.obj });
  }, [form]);

  const loadProxies = useCallback(async () => {
    const r = await HttpUtil.get<Proxy[]>('/panel/api/telemt/proxy');
    if (!r?.success || !Array.isArray(r.obj)) return;

    const unique = new Map<string, Proxy>();
    for (const item of r.obj) {
      const key = item.link || item.name;
      if (!unique.has(key)) unique.set(key, item);
    }
    setProxies(Array.from(unique.values()));
  }, []);

  const refreshAll = useCallback(async () => {
    setRefreshing(true);
    try {
      await Promise.all([loadStatus(), loadConfig(), loadProxies()]);
    } finally {
      setRefreshing(false);
    }
  }, [loadConfig, loadProxies, loadStatus]);

  useEffect(() => {
    const initialRefresh = window.setTimeout(() => void refreshAll(), 0);
    const timer = window.setInterval(() => void loadStatus(), 15000);
    return () => {
      window.clearTimeout(initialRefresh);
      window.clearInterval(timer);
    };
  }, [loadStatus, refreshAll]);

  const runAction = async (actionName: ServiceAction, successText: string) => {
    setServiceAction(actionName);
    try {
      const r = await HttpUtil.post(
        '/panel/api/telemt/action',
        { action: actionName },
        jsonOptions,
      );
      if (r?.success) {
        message.success(successText);
        await loadStatus();
        if (actionName === 'update') await loadConfig();
      } else {
        message.error(r?.msg || 'Команда не выполнена');
      }
    } finally {
      setServiceAction(null);
    }
  };

  const save = async (values: Config) => {
    setConfigSaving(true);
    try {
      const r = await HttpUtil.post('/panel/api/telemt/config', values, jsonOptions);
      if (r?.success) {
        message.success(
          status.active
            ? 'Настройки сохранены, Telemt перезапущен автоматически'
            : 'Настройки сохранены',
        );
        await refreshAll();
      } else {
        message.error(r?.msg || 'Не удалось сохранить конфигурацию');
      }
    } finally {
      setConfigSaving(false);
    }
  };

  const createProxy = async () => {
    setProxyCreating(true);
    try {
      const r = await HttpUtil.post<Proxy>(
        '/panel/api/telemt/proxy',
        { name: 'Telegram Proxy' },
        jsonOptions,
      );
      if (r?.success && r.obj) {
        message.success('Прокси создан и сервис применил его автоматически');
        await Promise.all([loadStatus(), loadProxies()]);
      } else {
        message.error(r?.msg || 'Не удалось создать прокси');
      }
    } finally {
      setProxyCreating(false);
    }
  };

  const deleteProxy = async (name: string) => {
    setProxyDeleting(name);
    try {
      const r = await HttpUtil.delete('/panel/api/telemt/proxy/' + encodeURIComponent(name));
      if (r?.success) {
        message.success('Прокси удалён');
        await Promise.all([loadStatus(), loadProxies()]);
      } else {
        message.error(r?.msg || 'Не удалось удалить прокси');
      }
    } finally {
      setProxyDeleting(null);
    }
  };

  const openWebProxy = () => {
    setWebProxyDomain(status.webProxy.domain || status.webProxy.defaultDomain || '');
    setWebProxyOpen(true);
  };

  const enableWebProxy = async () => {
    setWebProxyLoading(true);
    try {
      const r = await HttpUtil.post(
        '/panel/api/telemt/webproxy/enable',
        { domain: webProxyDomain.trim() },
        jsonOptions,
      );
      if (r?.success) {
        message.success(status.webProxy.enabled ? 'WEB Proxy перенастроен' : 'WEB Proxy поднят');
        setWebProxyOpen(false);
        await loadStatus();
      } else {
        message.error(r?.msg || 'Не удалось поднять WEB Proxy');
        await loadStatus();
      }
    } finally {
      setWebProxyLoading(false);
    }
  };

  const disableWebProxy = async () => {
    setWebProxyLoading(true);
    try {
      const r = await HttpUtil.post('/panel/api/telemt/webproxy/disable', {}, jsonOptions);
      if (r?.success) {
        message.success('WEB Proxy отключён');
        setWebProxyOpen(false);
        await loadStatus();
      } else {
        message.error(r?.msg || 'Не удалось отключить WEB Proxy');
        await loadStatus();
      }
    } finally {
      setWebProxyLoading(false);
    }
  };

  const generateSecret = () => {
    form.setFieldValue('secret', RandomUtil.randomSeq(32, { type: 'hex' }));
    message.success('Новый секрет сгенерирован');
  };

  const updateTelemt = () =>
    Modal.confirm({
      title: 'Обновить Telemt?',
      content:
        'Будет установлена версия ' +
        (status.latestVersion || 'из последнего доступного релиза') +
        '.',
      okText: 'Обновить',
      cancelText: 'Отмена',
      onOk: () => runAction('update', 'Telemt обновлён'),
    });

  const statusText = !status.installed
    ? 'Не установлен'
    : status.active
      ? 'Работает'
      : 'Остановлен';

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout className="page-layout telemt-page">
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content className="content-area">
            <div className="telemt-shell">
              <div className="telemt-header">
                <div className="telemt-header-main">
                  <Space size={10} wrap>
                    <Typography.Title level={2} className="telemt-title">
                      Telemt
                    </Typography.Title>
                    <Tag
                      color={status.active ? 'green' : status.installed ? 'default' : 'red'}
                      icon={status.active ? <CheckCircleOutlined /> : undefined}
                    >
                      {statusText}
                    </Tag>
                    {status.webProxy.enabled && (
                      <Tag color="blue">WEB Proxy · {status.webProxy.domain}</Tag>
                    )}
                  </Space>
                  <Typography.Text type="secondary">
                    MTProto-прокси без лишних ручных действий: сервис сам применяет изменения.
                  </Typography.Text>
                </div>
                <Space wrap>
                  <Button
                    htmlType="button"
                    icon={<ReloadOutlined />}
                    loading={refreshing}
                    onClick={() => void refreshAll()}
                  >
                    Обновить
                  </Button>
                </Space>
              </div>

              {!status.installed && (
                <Alert
                  className="telemt-card"
                  type="warning"
                  showIcon
                  message="Telemt не установлен"
                  description="Установите актуальную сборку 3X-UI. После установки эта страница сама подхватит бинарник и службу."
                />
              )}

              <Row gutter={[16, 16]}>
                <Col xs={24} lg={16}>
                  <Card
                    className="telemt-card telemt-service-card"
                    title={
                      <Space>
                        <ApiOutlined />
                        Сервис
                      </Space>
                    }
                    extra={
                      <Space size={8}>
                        <Typography.Text type="secondary">Автозапуск</Typography.Text>
                        <Switch
                          size="small"
                          checked={status.enabled}
                          disabled={!status.installed || serviceAction !== null}
                          loading={serviceAction === 'enable' || serviceAction === 'disable'}
                          onChange={(checked) =>
                            void runAction(
                              checked ? 'enable' : 'disable',
                              checked ? 'Автозапуск включён' : 'Автозапуск выключен',
                            )
                          }
                        />
                      </Space>
                    }
                  >
                    <Row gutter={[16, 16]}>
                      <Col xs={24} md={8}>
                        <div className="telemt-metric">
                          <Typography.Text type="secondary">Версия</Typography.Text>
                          <Typography.Title level={4}>{status.version || '—'}</Typography.Title>
                          {status.updateAvailable && (
                            <Typography.Text type="warning">
                              Доступно {status.latestVersion}
                            </Typography.Text>
                          )}
                        </div>
                      </Col>
                      <Col xs={24} md={8}>
                        <div className="telemt-metric">
                          <Typography.Text type="secondary">Конфигурация</Typography.Text>
                          <Typography.Title level={4}>
                            <Tag color={status.configured ? 'green' : 'default'}>
                              {status.configured ? 'Готова' : 'Не создана'}
                            </Tag>
                          </Typography.Title>
                          <Typography.Text type="secondary">
                            Порт {configuredPort || defaults.port}
                          </Typography.Text>
                        </div>
                      </Col>
                    </Row>

                    <Space wrap className="telemt-service-actions">
                      {status.active ? (
                        <Popconfirm
                          title="Остановить Telemt?"
                          description="Обычный и WEB Proxy трафик перестанут обслуживаться до запуска."
                          okText="Остановить"
                          cancelText="Отмена"
                          onConfirm={() => void runAction('stop', 'Telemt остановлен')}
                        >
                          <Button
                            danger
                            icon={<StopOutlined />}
                            loading={serviceAction === 'stop'}
                            disabled={serviceAction !== null}
                          >
                            Остановить
                          </Button>
                        </Popconfirm>
                      ) : (
                        <Button
                          type="primary"
                          icon={<CheckCircleOutlined />}
                          loading={serviceAction === 'start'}
                          disabled={!status.installed || serviceAction !== null}
                          onClick={() => void runAction('start', 'Telemt запущен')}
                        >
                          Запустить
                        </Button>
                      )}
                      <Button
                        icon={<SyncOutlined />}
                        loading={serviceAction === 'restart'}
                        disabled={!status.installed || serviceAction !== null}
                        onClick={() => void runAction('restart', 'Telemt перезапущен')}
                      >
                        Перезапустить
                      </Button>
                    </Space>
                  </Card>
                </Col>

                <Col xs={24} lg={8}>
                  <Card
                    className="telemt-card telemt-web-card"
                    title={
                      <Space>
                        <LinkOutlined />
                        Web Proxy
                      </Space>
                    }
                    extra={
                      status.webProxy.enabled ? (
                        <Tag color="green">Активен</Tag>
                      ) : (
                        <Tag>Не настроен</Tag>
                      )
                    }
                  >
                    {status.webProxy.enabled ? (
                      <>
                        <Typography.Text strong>{status.webProxy.domain}</Typography.Text>
                        <Typography.Paragraph type="secondary" style={{ margin: '6px 0 12px' }}>
                          HTTPS :443 → Telemt :{status.webProxy.listenPort}
                        </Typography.Paragraph>
                        <div className="telemt-link-box">
                          <Typography.Text code ellipsis>
                            {status.webProxy.link}
                          </Typography.Text>
                          <Button
                            type="text"
                            icon={<CopyOutlined />}
                            aria-label="Копировать ссылку WEB Proxy"
                            onClick={() => void navigator.clipboard.writeText(status.webProxy.link)}
                          />
                        </div>
                        <Space wrap className="telemt-web-meta">
                          <Tag color={status.webProxy.certificateReady ? 'green' : 'warning'}>
                            TLS {status.webProxy.certificateReady ? 'готов' : 'требуется'}
                          </Tag>
                          <Tag color={status.webProxy.nginxActive ? 'green' : 'warning'}>
                            nginx {status.webProxy.nginxActive ? 'работает' : 'остановлен'}
                          </Tag>
                        </Space>
                      </>
                    ) : (
                      <Typography.Paragraph type="secondary">
                        Домен → HTTPS :443 → nginx → локальный WEB transport Telemt. Заглушка и TLS
                        настраиваются автоматически.
                      </Typography.Paragraph>
                    )}
                    {status.webProxy.error && (
                      <Alert
                        className="telemt-alert-compact"
                        type={status.webProxy.enabled ? 'warning' : 'info'}
                        showIcon
                        message={status.webProxy.error}
                      />
                    )}
                    <Button
                      block
                      type="primary"
                      ghost={status.webProxy.enabled}
                      icon={<LinkOutlined />}
                      disabled={!status.installed || !status.webProxy.supported}
                      loading={webProxyLoading}
                      onClick={openWebProxy}
                    >
                      {status.webProxy.enabled ? 'Настроить Web Proxy' : 'Поднять Web Proxy'}
                    </Button>
                  </Card>
                </Col>
              </Row>

              <Card
                className="telemt-card telemt-update-card"
                title={
                  <Space>
                    <DownloadOutlined />
                    Обновление Telemt
                  </Space>
                }
              >
                <Row gutter={[16, 16]} align="middle">
                  <Col xs={24} md={8}>
                    <Typography.Text type="secondary">Установленная версия</Typography.Text>
                    <Typography.Title level={4} style={{ margin: '4px 0 0' }}>
                      {status.installed ? status.version || 'не определена' : 'не установлена'}
                    </Typography.Title>
                  </Col>
                  <Col xs={24} md={8}>
                    <Typography.Text type="secondary">Последняя версия</Typography.Text>
                    <Typography.Title level={4} style={{ margin: '4px 0 0' }}>
                      {status.latestVersion || 'проверка недоступна'}
                    </Typography.Title>
                  </Col>
                  <Col xs={24} md={8}>
                    <Space wrap>
                      {status.updateAvailable ? (
                        <Button
                          type="primary"
                          icon={<DownloadOutlined />}
                          loading={serviceAction === 'update'}
                          disabled={!status.installed || serviceAction !== null}
                          onClick={updateTelemt}
                        >
                          Обновить до {status.latestVersion}
                        </Button>
                      ) : (
                        <Button
                          icon={<ReloadOutlined />}
                          disabled={!status.installed || serviceAction !== null}
                          onClick={() => void loadStatus()}
                        >
                          Проверить обновление
                        </Button>
                      )}
                    </Space>
                  </Col>
                </Row>

                {!status.installed ? (
                  <Alert
                    style={{ marginTop: 16 }}
                    type="info"
                    showIcon
                    message="Telemt ещё не установлен"
                    description="После установки панель автоматически покажет текущую и последнюю доступную версию."
                  />
                ) : status.updateAvailable ? (
                  <Alert
                    style={{ marginTop: 16 }}
                    type="warning"
                    showIcon
                    message={'Доступно обновление до ' + status.latestVersion}
                    description="Обновление выполняется встроенным Telemt updater и после него возвращает сервис в рабочее состояние."
                  />
                ) : status.latestVersion ? (
                  <Alert
                    style={{ marginTop: 16 }}
                    type="success"
                    showIcon
                    message="Установлена последняя версия Telemt"
                    description="Новых опубликованных релизов для текущей проверки не найдено."
                  />
                ) : (
                  <Alert
                    style={{ marginTop: 16 }}
                    type="info"
                    showIcon
                    message="Не удалось получить последнюю версию"
                    description="Проверьте соединение сервера с GitHub и нажмите «Проверить» ещё раз."
                  />
                )}
              </Card>

              <Card
                className="telemt-card"
                title={
                  <Space>
                    <PlusOutlined />
                    Прокси
                    <Tag>{proxies.length}</Tag>
                  </Space>
                }
                extra={
                  <Button
                    type="primary"
                    icon={<PlusOutlined />}
                    loading={proxyCreating}
                    disabled={!status.installed}
                    onClick={() => void createProxy()}
                  >
                    Создать прокси
                  </Button>
                }
              >
                <Typography.Paragraph type="secondary" className="telemt-card-hint">
                  Адрес сервера определяется автоматически. После создания Telemt сам применит
                  пользователя и перезапустит службу только когда это действительно требуется.
                </Typography.Paragraph>

                {proxies.length === 0 ? (
                  <div className="telemt-empty-state">
                    <Typography.Text type="secondary">Прокси пока не созданы.</Typography.Text>
                  </div>
                ) : (
                  <div className="telemt-proxy-list">
                    {proxies.map((item) => (
                      <div className="telemt-proxy-item" key={item.name}>
                        <div className="telemt-proxy-main">
                          <Space size={8} wrap>
                            <Typography.Text strong>{item.name}</Typography.Text>
                            <Tag>
                              {item.host}:{item.port}
                            </Tag>
                            {item.tls && <Tag color="blue">Fake-TLS</Tag>}
                          </Space>
                          <Typography.Paragraph
                            copyable={{ text: item.link, tooltips: ['Копировать', 'Скопировано'] }}
                            ellipsis={{ rows: 1 }}
                            code
                            style={{ margin: '5px 0 0' }}
                          >
                            {item.link}
                          </Typography.Paragraph>
                        </div>
                        <Popconfirm
                          title="Удалить прокси?"
                          okText="Удалить"
                          cancelText="Отмена"
                          onConfirm={() => void deleteProxy(item.name)}
                        >
                          <Button
                            danger
                            type="text"
                            icon={<DeleteOutlined />}
                            aria-label={'Удалить прокси ' + item.name}
                            loading={proxyDeleting === item.name}
                          />
                        </Popconfirm>
                      </div>
                    ))}
                  </div>
                )}
              </Card>

              <Collapse
                className="telemt-card telemt-advanced"
                items={[
                  {
                    key: 'settings',
                    label: (
                      <Space>
                        <SettingOutlined />
                        Дополнительные настройки
                      </Space>
                    ),
                    children: (
                      <Form form={form} layout="vertical" onFinish={save}>
                        <Row gutter={16}>
                          <Col xs={24} md={8}>
                            <Form.Item
                              name="port"
                              label="Порт Telemt"
                              rules={[{ required: true }, { type: 'number', min: 1, max: 65535 }]}
                            >
                              <InputNumber style={{ width: '100%' }} />
                            </Form.Item>
                          </Col>
                          <Col xs={24} md={16}>
                            <Form.Item label="Основной секрет" required>
                              <Space.Compact block>
                                <Form.Item
                                  name="secret"
                                  noStyle
                                  rules={[
                                    { required: true },
                                    {
                                      pattern: /^[0-9a-fA-F]{32}$/,
                                      message: 'Нужно ровно 32 hex-символа',
                                    },
                                  ]}
                                >
                                  <Input placeholder="32 hex-символа" autoComplete="off" />
                                </Form.Item>
                                <Button
                                  htmlType="button"
                                  icon={<SafetyCertificateOutlined />}
                                  onClick={generateSecret}
                                  disabled={configSaving}
                                >
                                  Сгенерировать
                                </Button>
                              </Space.Compact>
                            </Form.Item>
                          </Col>
                        </Row>

                        <Row gutter={16}>
                          <Col xs={24} md={12}>
                            <Form.Item name="sni" label="SNI / домен маскировки">
                              <Input placeholder="petrovich.ru" />
                            </Form.Item>
                          </Col>
                          <Col xs={12} sm={8} md={4}>
                            <Form.Item name="tls" label="Fake-TLS" valuePropName="checked">
                              <Switch />
                            </Form.Item>
                          </Col>
                          <Col xs={12} sm={8} md={4}>
                            <Form.Item name="fastMode" label="Fast mode" valuePropName="checked">
                              <Switch />
                            </Form.Item>
                          </Col>
                          <Col xs={12} sm={8} md={4}>
                            <Form.Item name="ipv4" label="IPv4" valuePropName="checked">
                              <Switch />
                            </Form.Item>
                          </Col>
                        </Row>

                        <Row gutter={16}>
                          <Col xs={12} md={6}>
                            <Form.Item name="ipv6" label="IPv6" valuePropName="checked">
                              <Switch />
                            </Form.Item>
                          </Col>
                          <Col xs={12} md={6}>
                            <Form.Item name="classic" label="Classic" valuePropName="checked">
                              <Switch />
                            </Form.Item>
                          </Col>
                          <Col xs={12} md={6}>
                            <Form.Item name="secure" label="Secure" valuePropName="checked">
                              <Switch />
                            </Form.Item>
                          </Col>
                        </Row>

                        <Space wrap>
                          <Button
                            type="primary"
                            htmlType="submit"
                            loading={configSaving}
                            disabled={!status.installed}
                          >
                            Сохранить и применить
                          </Button>
                          <Typography.Text type="secondary">
                            При работающем сервисе перезапуск выполняется автоматически.
                          </Typography.Text>
                        </Space>
                      </Form>
                    ),
                  },
                ]}
              />

              <Typography.Paragraph type="secondary" className="telemt-footnote">
                Web Proxy использует отдельный локальный WEB transport на порту{' '}
                {status.webProxy.listenPort || 15080}; наружу публикуется только HTTPS :443.
              </Typography.Paragraph>
            </div>
          </Layout.Content>
        </Layout>

        <Modal
          open={webProxyOpen}
          title={status.webProxy.enabled ? 'Настройка Web Proxy' : 'Поднять Web Proxy'}
          onCancel={() => {
            if (!webProxyLoading) setWebProxyOpen(false);
          }}
          footer={[
            <Button key="close" onClick={() => setWebProxyOpen(false)} disabled={webProxyLoading}>
              Закрыть
            </Button>,
            ...(status.webProxy.enabled
              ? [
                  <Popconfirm
                    key="disable"
                    title="Отключить Web Proxy?"
                    description="nginx-конфигурация будет убрана, обычный Telemt останется работать."
                    okText="Отключить"
                    cancelText="Отмена"
                    onConfirm={() => void disableWebProxy()}
                  >
                    <Button danger loading={webProxyLoading}>
                      Отключить
                    </Button>
                  </Popconfirm>,
                ]
              : []),
            <Button
              key="save"
              type="primary"
              onClick={() => void enableWebProxy()}
              loading={webProxyLoading}
              disabled={
                !status.installed ||
                !status.webProxy.supported ||
                !webProxyDomain.trim() ||
                (!status.webProxy.port443Available && !status.webProxy.nginxActive)
              }
            >
              {status.webProxy.enabled ? 'Применить' : 'Поднять'}
            </Button>,
          ]}
        >
          <Space direction="vertical" size={14} style={{ width: '100%' }}>
            <Alert
              type="info"
              showIcon
              message="Панель всё сделает сама"
              description="nginx, HTTPS-сертификат, статическая заглушка и локальный WEB transport Telemt будут настроены автоматически. Ручной запуск сервисов после установки не нужен."
            />

            <Form layout="vertical">
              <Form.Item
                label="Домен"
                help={'По умолчанию: ' + (status.webProxy.defaultDomain || 'не определён')}
              >
                <Input
                  value={webProxyDomain}
                  onChange={(e) => setWebProxyDomain(e.target.value)}
                  placeholder="web.example.com"
                  disabled={webProxyLoading}
                  addonAfter=":443"
                />
              </Form.Item>
            </Form>

            <Descriptions size="small" column={1} bordered>
              <Descriptions.Item label="Telemt WEB">
                127.0.0.1:{status.webProxy.listenPort || 15080}
              </Descriptions.Item>
              <Descriptions.Item label="nginx">
                {status.webProxy.nginxActive ? 'запущен' : 'будет запущен автоматически'}
              </Descriptions.Item>
              <Descriptions.Item label="TLS">
                {status.webProxy.certificateReady
                  ? 'сертификат уже готов'
                  : 'сертификат будет получен автоматически'}
              </Descriptions.Item>
              <Descriptions.Item label="443">
                {status.webProxy.nginxActive && status.webProxy.port443Owner === 'nginx'
                  ? 'порт уже использует nginx — это нормально'
                  : status.webProxy.port443Available
                    ? 'порт свободен'
                    : status.webProxy.port443Owner
                      ? 'занят: ' + status.webProxy.port443Owner
                      : 'занят другим сервисом'}
              </Descriptions.Item>
              {status.webProxy.port443Owner && status.webProxy.port443Owner !== 'nginx' && (
                <Descriptions.Item label="Причина">
                  Остановите «{status.webProxy.port443Owner}» или перенесите его с порта 443.
                </Descriptions.Item>
              )}
            </Descriptions>

            {status.webProxy.error && (
              <Alert type="warning" showIcon message={status.webProxy.error} />
            )}

            {status.webProxy.enabled && status.webProxy.link && (
              <div>
                <Typography.Text strong>Ссылка для Telegram</Typography.Text>
                <div className="telemt-link-box telemt-link-box-large">
                  <Typography.Text code>{status.webProxy.link}</Typography.Text>
                  <Button
                    type="text"
                    icon={<CopyOutlined />}
                    onClick={() => void navigator.clipboard.writeText(status.webProxy.link)}
                  >
                    Копировать
                  </Button>
                </div>
              </div>
            )}
          </Space>
        </Modal>
      </Layout>
    </ConfigProvider>
  );
}
