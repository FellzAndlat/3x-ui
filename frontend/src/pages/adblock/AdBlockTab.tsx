import { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Card,
  Col,
  Collapse,
  Input,
  Radio,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';

type ApiMsg<T = unknown> = {
  success?: boolean;
  msg?: string;
  obj?: T;
};

type AdBlockProfile = {
  id: string;
  name: string;
  description: string;
  sources: string;
  updateIntervalHours: number;
};

type AdBlockScope = {
  inboundMode: 'all' | 'include' | 'exclude';
  inbounds: string[];
  clientMode: 'all' | 'include' | 'exclude';
  clients: string[];
};

type AdBlockPolicy = {
  id: string;
  name: string;
  enabled: boolean;
  profile: string;
  scope: AdBlockScope;
};

type SourceStatus = {
  url: string;
  domainCount: number;
  updatedAt: string;
  checkedAt: string;
  lastError: string;
  stale: boolean;
};

type ApplicationStatus = {
  pending: boolean;
  lastError: string;
  appliedAt: string;
  nextRetry: string;
};

type ServerSettings = {
  enabled: boolean;
  outbound: string;
  limits: {
    maxConnections: number;
    workers: number;
    bodyMiB: number;
    queueMs: number;
  };
};

type AdBlockStatus = {
  server?: ServerSettings;
  policies: AdBlockPolicy[];
  scope: AdBlockScope;
  scopeOptions: {
    inbounds: { value: string; label: string }[];
    clients: { value: string; label: string }[];
  };
  application: ApplicationStatus;
  pausedUntil: string;
  sourceStatuses: SourceStatus[];
  enabled: boolean;
  sources: string;
  customDomains: string;
  allowlist: string;
  autoUpdate: boolean;
  updateIntervalHours: number;
  lastUpdate: string;
  domainCount: number;
  sourceCount: number;
  profile: string;
  profiles: AdBlockProfile[];
  youtubeMode: 'off' | 'compatible' | 'privacy';
  automation: {
    lastAttempt: string;
    lastError: string;
    retryCount: number;
    nextRetry: string;
  };
};

type AdBlockUpdateResponse = {
  status: AdBlockStatus;
  update?: {
    changed?: boolean;
  };
};

const defaultScope: AdBlockScope = {
  inboundMode: 'all',
  inbounds: [],
  clientMode: 'all',
  clients: [],
};

const emptyStatus: AdBlockStatus = {
  policies: [],
  scope: defaultScope,
  scopeOptions: { inbounds: [], clients: [] },
  application: { pending: false, lastError: '', appliedAt: '', nextRetry: '' },
  pausedUntil: '',
  sourceStatuses: [],
  enabled: false,
  sources: '',
  customDomains: '',
  allowlist: '',
  autoUpdate: true,
  updateIntervalHours: 24,
  lastUpdate: '',
  domainCount: 0,
  sourceCount: 0,
  profile: 'custom',
  profiles: [],
  youtubeMode: 'off',
  automation: { lastAttempt: '', lastError: '', retryCount: 0, nextRetry: '' },
};

function settingsValue(status: AdBlockStatus) {
  return {
    server: status.server,
    profile: status.profile,
    youtubeMode: status.youtubeMode,
    enabled: status.enabled,
    sources: status.sources,
    customDomains: status.customDomains,
    allowlist: status.allowlist,
    autoUpdate: status.autoUpdate,
    updateIntervalHours: status.updateIntervalHours,
    scope: status.scope,
    policies: status.policies,
  };
}

function scopeEditor(
  scope: AdBlockScope,
  options: AdBlockStatus['scopeOptions'],
  busy: boolean,
  onChange: (scope: AdBlockScope) => void,
) {
  return (
    <Row gutter={[16, 12]}>
      {(['inbound', 'client'] as const).map((kind) => {
        const modeKey = kind === 'inbound' ? 'inboundMode' : 'clientMode';
        const itemsKey = kind === 'inbound' ? 'inbounds' : 'clients';
        const mode = scope[modeKey];
        const values = scope[itemsKey];

        return (
          <Col xs={24} md={12} key={kind}>
            <Typography.Text>
              {kind === 'inbound' ? 'Входящие подключения' : 'Клиенты'}
            </Typography.Text>
            <Select
              style={{ width: '100%', marginTop: 8 }}
              disabled={busy}
              value={mode}
              options={[
                { value: 'all', label: 'Все' },
                { value: 'include', label: 'Только выбранные' },
                { value: 'exclude', label: 'Все, кроме выбранных' },
              ]}
              onChange={(nextMode) =>
                onChange({
                  ...scope,
                  [modeKey]: nextMode,
                  [itemsKey]: nextMode === 'all' ? [] : values,
                })
              }
            />
            {mode !== 'all' && (
              <Select
                mode="tags"
                style={{ width: '100%', marginTop: 8 }}
                disabled={busy}
                value={values}
                options={options[itemsKey]}
                placeholder="Выберите из списка"
                onChange={(items) => onChange({ ...scope, [itemsKey]: items })}
              />
            )}
            {mode === 'include' && values.length === 0 && (
              <Typography.Text type="warning">
                Ничего не выбрано — правило применяться не будет.
              </Typography.Text>
            )}
          </Col>
        );
      })}
    </Row>
  );
}

export default function AdBlockTab() {
  const [status, setStatus] = useState<AdBlockStatus>(emptyStatus);
  const [applied, setApplied] = useState<AdBlockStatus>(emptyStatus);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [saving, setSaving] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [acting, setActing] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  const [messageApi, contextHolder] = message.useMessage();

  useEffect(() => {
    const controller = new AbortController();
    void HttpUtil.get<AdBlockStatus>('/panel/api/adblock/status', undefined, {
      silent: true,
      signal: controller.signal,
    })
      .then((response) => {
        if (controller.signal.aborted) return;
        if (response.success && response.obj) {
          setStatus(response.obj);
          setApplied(response.obj);
        } else {
          setLoadFailed(true);
          messageApi.error(response.msg || 'Не удалось получить настройки AdBlock');
        }
        setLoading(false);
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setLoadFailed(true);
        messageApi.error(
          error instanceof Error ? error.message : 'Не удалось получить настройки AdBlock',
        );
        setLoading(false);
      });
    return () => controller.abort();
  }, [messageApi, loadAttempt]);

  const refreshStatus = async () => {
    const response = await HttpUtil.get<AdBlockStatus>('/panel/api/adblock/status', undefined, {
      silent: true,
    });
    if (response.success && response.obj) {
      setApplied(response.obj);
      setStatus((prev) => ({ ...response.obj!, ...settingsValue(prev) }));
    }
  };

  const save = async () => {
    setSaving(true);
    try {
      const response = (await HttpUtil.post('/panel/api/adblock/settings', settingsValue(status), {
        silent: true,
        timeout: 330_000,
      })) as ApiMsg<AdBlockStatus>;

      if (!response?.success || !response.obj) {
        await refreshStatus();
        messageApi.error(response?.msg || 'Не удалось сохранить настройки AdBlock');
        return;
      }
      setStatus(response.obj);
      setApplied(response.obj);
      messageApi.success('Настройки AdBlock сохранены');
    } catch (error) {
      await refreshStatus().catch(() => {});
      messageApi.error(
        error instanceof Error ? error.message : 'Не удалось сохранить настройки AdBlock',
      );
    } finally {
      setSaving(false);
    }
  };

  const updateLists = async () => {
    setUpdating(true);
    try {
      const response = (await HttpUtil.post('/panel/api/adblock/update', undefined, {
        silent: true,
        timeout: 330_000,
      })) as ApiMsg<AdBlockUpdateResponse>;

      if (!response?.success || !response.obj?.status) {
        await refreshStatus().catch(() => {});
        messageApi.error(response?.msg || 'Не удалось обновить списки AdBlock');
        return;
      }

      setStatus(response.obj.status);
      setApplied(response.obj.status);
      messageApi.success(
        response.obj.update?.changed === false
          ? 'Списки проверены, изменений нет'
          : 'Списки AdBlock обновлены',
      );
    } catch (error) {
      await refreshStatus().catch(() => {});
      messageApi.error(
        error instanceof Error ? error.message : 'Не удалось обновить списки AdBlock',
      );
    } finally {
      setUpdating(false);
    }
  };

  const runAction = async (path: 'apply' | 'pause', body?: { minutes: number }) => {
    setActing(true);
    try {
      const response = (await HttpUtil.post(`/panel/api/adblock/${path}`, body, {
        silent: true,
        timeout: 330_000,
      })) as ApiMsg<AdBlockStatus>;

      if (!response.success || !response.obj) {
        await refreshStatus();
        messageApi.error(response.msg || 'Не удалось применить настройки');
        return;
      }

      setStatus(response.obj);
      setApplied(response.obj);
      setNow(Date.now());
      messageApi.success(path === 'apply' ? 'Настройки применены' : 'Состояние AdBlock изменено');
    } catch (error) {
      await refreshStatus().catch(() => {});
      messageApi.error(error instanceof Error ? error.message : 'Ошибка применения');
    } finally {
      setActing(false);
    }
  };

  const applyProfile = (id: string) => {
    const profile = status.profiles.find((item) => item.id === id);
    if (!profile) return;

    setStatus((prev) => ({
      ...prev,
      profile: profile.id,
      sources: profile.sources,
      autoUpdate: true,
      updateIntervalHours: profile.updateIntervalHours,
    }));
  };

  const busy = loading || loadFailed || saving || updating || acting;
  const dirty = JSON.stringify(settingsValue(status)) !== JSON.stringify(settingsValue(applied));
  const paused = !!applied.pausedUntil && new Date(applied.pausedUntil).getTime() > now;
  const activeProfile = status.profiles.find((profile) => profile.id === status.profile);

  const profileOptions = useMemo(
    () =>
      status.profiles.map((profile) => ({
        label: profile.name,
        value: profile.id,
      })),
    [status.profiles],
  );

  useEffect(() => {
    if (loading || loadFailed || saving || updating || acting) return;
    const controller = new AbortController();
    const timer = window.setInterval(() => {
      setNow(Date.now());
      if (document.hidden) return;
      void HttpUtil.get<AdBlockStatus>('/panel/api/adblock/status', undefined, {
        silent: true,
        signal: controller.signal,
      })
        .then((response) => {
          if (controller.signal.aborted || !response.success || !response.obj) return;
          setApplied(response.obj);
          setStatus((prev) =>
            dirty ? { ...response.obj!, ...settingsValue(prev) } : response.obj!,
          );
        })
        .catch(() => {});
    }, 15_000);

    return () => {
      controller.abort();
      window.clearInterval(timer);
    };
  }, [loading, loadFailed, saving, updating, acting, dirty]);

  const advancedItems = [
    {
      key: 'custom',
      label: 'Расширенные настройки',
      children: (
        <Space orientation="vertical" size={20} style={{ width: '100%' }}>
          <div>
            <Typography.Text strong>Свои источники</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
              Для большинства пользователей этот раздел не нужен. Один HTTP/HTTPS URL на строку.
              При ручном изменении набор становится пользовательским.
            </Typography.Paragraph>
            <Input.TextArea
              disabled={busy}
              value={status.sources}
              rows={5}
              placeholder={'https://example.org/hosts.txt\nhttps://example.org/domains.txt'}
              onChange={(event) =>
                setStatus((prev) => ({
                  ...prev,
                  sources: event.target.value,
                  profile: 'custom',
                  autoUpdate: true,
                }))
              }
            />
          </div>

          <Row gutter={[16, 16]}>
            <Col xs={24} md={12}>
              <Typography.Text strong>Дополнительно блокировать</Typography.Text>
              <Typography.Paragraph type="secondary">
                Один домен на строку. Для домена вместе с поддоменами используйте{' '}
                <Typography.Text code>domain:example.com</Typography.Text>.
              </Typography.Paragraph>
              <Input.TextArea
                disabled={busy}
                value={status.customDomains}
                rows={5}
                placeholder={'ads.example.com\ntracker.example.net'}
                onChange={(event) =>
                  setStatus((prev) => ({ ...prev, customDomains: event.target.value }))
                }
              />
            </Col>
            <Col xs={24} md={12}>
              <Typography.Text strong>Разрешённые домены</Typography.Text>
              <Typography.Paragraph type="secondary">
                Добавьте сюда сайт, если готовый список мешает его работе.
              </Typography.Paragraph>
              <Input.TextArea
                disabled={busy}
                value={status.allowlist}
                rows={5}
                placeholder={'example.com\ncdn.example.org'}
                onChange={(event) =>
                  setStatus((prev) => ({ ...prev, allowlist: event.target.value }))
                }
              />
            </Col>
          </Row>

          <div>
            <Typography.Text strong>Где применять AdBlock</Typography.Text>
            <Typography.Paragraph type="secondary">
              По умолчанию фильтрация работает для всех подключений. Меняйте этот раздел только
              если нужно исключить отдельный inbound или клиента.
            </Typography.Paragraph>
            {scopeEditor(status.scope, status.scopeOptions, busy, (scope) =>
              setStatus((prev) => ({ ...prev, scope }))
            )}
          </div>

          <div>
            <Typography.Text strong>Индивидуальные правила</Typography.Text>
            <Typography.Paragraph type="secondary">
              Необязательно. Позволяет назначить другой готовый набор конкретному inbound или
              клиенту.
            </Typography.Paragraph>
            <Space orientation="vertical" style={{ width: '100%' }}>
              {status.policies.map((policy, index) => {
                const patchPolicy = (update: Partial<AdBlockPolicy>) =>
                  setStatus((prev) => ({
                    ...prev,
                    policies: prev.policies.map((item) =>
                      item.id === policy.id ? { ...item, ...update } : item,
                    ),
                  }));

                return (
                  <Card
                    size="small"
                    key={policy.id}
                    title={
                      <Space>
                        <Switch
                          size="small"
                          disabled={busy}
                          checked={policy.enabled}
                          onChange={(enabled) => patchPolicy({ enabled })}
                        />
                        <span>{policy.name || `Правило ${index + 1}`}</span>
                      </Space>
                    }
                    extra={
                      <Button
                        size="small"
                        danger
                        disabled={busy}
                        onClick={() =>
                          setStatus((prev) => ({
                            ...prev,
                            policies: prev.policies.filter((item) => item.id !== policy.id),
                          }))
                        }
                      >
                        Удалить
                      </Button>
                    }
                  >
                    <Row gutter={[16, 12]}>
                      <Col xs={24} md={12}>
                        <Typography.Text>Название</Typography.Text>
                        <Input
                          style={{ marginTop: 8 }}
                          disabled={busy}
                          value={policy.name}
                          maxLength={64}
                          onChange={(event) => patchPolicy({ name: event.target.value })}
                        />
                      </Col>
                      <Col xs={24} md={12}>
                        <Typography.Text>Набор</Typography.Text>
                        <Select
                          style={{ width: '100%', marginTop: 8 }}
                          disabled={busy}
                          value={policy.profile}
                          options={[
                            ...profileOptions,
                            { label: 'Без фильтрации', value: 'off' },
                          ]}
                          onChange={(profile) => patchPolicy({ profile })}
                        />
                      </Col>
                    </Row>
                    <div style={{ marginTop: 16 }}>
                      {scopeEditor(policy.scope, status.scopeOptions, busy, (scope) =>
                        patchPolicy({ scope })
                      )}
                    </div>
                  </Card>
                );
              })}

              <Button
                disabled={busy || status.policies.length >= 8}
                onClick={() =>
                  setStatus((prev) => ({
                    ...prev,
                    policies: [
                      ...prev.policies,
                      {
                        id: `p${Array.from(crypto.getRandomValues(new Uint32Array(2))).join('_')}`,
                        name: `Правило ${prev.policies.length + 1}`,
                        enabled: true,
                        profile: prev.profiles[0]?.id || 'balanced',
                        scope: {
                          inboundMode: 'all',
                          inbounds: [],
                          clientMode: 'include',
                          clients: [],
                        },
                      },
                    ],
                  }))
                }
              >
                Добавить индивидуальное правило
              </Button>
            </Space>
          </div>

          {status.server && (
            <div>
              <Typography.Text strong>Экспериментальная серверная фильтрация</Typography.Text>
              <Typography.Paragraph type="secondary">
                Для обычной блокировки рекламы эта функция не нужна. Оставьте выключенной, если
                специально её не настраивали.
              </Typography.Paragraph>
              <Space>
                <Switch
                  disabled={busy}
                  checked={status.server.enabled}
                  onChange={(enabled) =>
                    setStatus((prev) =>
                      prev.server ? { ...prev, server: { ...prev.server, enabled } } : prev,
                    )
                  }
                />
                <span>{status.server.enabled ? 'Включена' : 'Выключена'}</span>
              </Space>
            </div>
          )}
        </Space>
      ),
    },
    {
      key: 'diagnostics',
      label: 'Состояние источников',
      children:
        applied.sourceStatuses.length === 0 ? (
          <Typography.Text type="secondary">Источники ещё не загружались.</Typography.Text>
        ) : (
          <Table
            size="small"
            rowKey="url"
            pagination={false}
            scroll={{ x: 720 }}
            dataSource={applied.sourceStatuses}
            columns={[
              {
                title: 'Источник',
                dataIndex: 'url',
                render: (url: string) => (
                  <Typography.Text style={{ overflowWrap: 'anywhere' }}>{url}</Typography.Text>
                ),
              },
              { title: 'Доменов', dataIndex: 'domainCount' },
              {
                title: 'Обновлён',
                dataIndex: 'updatedAt',
                render: (value: string) => (value ? new Date(value).toLocaleString() : '—'),
              },
              {
                title: 'Состояние',
                dataIndex: 'lastError',
                render: (value: string) =>
                  value ? (
                    <Typography.Text type="warning">{value}</Typography.Text>
                  ) : (
                    <Tag color="success">Актуален</Tag>
                  ),
              },
            ]}
          />
        ),
    },
  ];

  return (
    <>
      {contextHolder}
      {loadFailed && (
        <Alert
          type="error"
          showIcon
          title="Не удалось загрузить настройки AdBlock"
          action={
            <Button
              onClick={() => {
                setLoading(true);
                setLoadFailed(false);
                setLoadAttempt((prev) => prev + 1);
              }}
            >
              Повторить
            </Button>
          }
        />
      )}

      <Spin spinning={loading}>
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <div>
            <Typography.Title level={4} style={{ margin: 0 }}>
              AdBlock
            </Typography.Title>
            <Typography.Text type="secondary">
              Простая серверная блокировка рекламы и трекеров для Xray и sing-box.
            </Typography.Text>
          </div>

          <Alert
            type={
              applied.application.pending
                ? 'warning'
                : applied.enabled && !paused
                  ? 'success'
                  : 'info'
            }
            showIcon
            title={
              applied.application.pending
                ? 'Изменения ожидают применения'
                : paused
                  ? 'AdBlock временно приостановлен'
                  : applied.enabled
                    ? 'AdBlock работает'
                    : 'AdBlock выключен'
            }
            description={
              applied.application.pending
                ? applied.application.lastError || 'Панель повторит применение автоматически.'
                : paused
                  ? `Возобновление: ${new Date(applied.pausedUntil).toLocaleString()}`
                  : applied.enabled
                    ? `${applied.domainCount.toLocaleString()} доменов из ${applied.sourceCount} источников`
                    : 'Включите AdBlock и выберите готовый список ниже.'
            }
            action={
              applied.application.pending ? (
                <Button
                  size="small"
                  loading={acting}
                  disabled={busy || dirty}
                  onClick={() => void runAction('apply')}
                >
                  Применить
                </Button>
              ) : undefined
            }
          />

          {applied.automation.lastError && (
            <Alert
              type="warning"
              showIcon
              title="Не удалось обновить один из списков"
              description={
                <>
                  Рабочая копия продолжает использоваться. {applied.automation.lastError}
                  {applied.automation.nextRetry && (
                    <div>
                      Следующая автоматическая попытка:{' '}
                      {new Date(applied.automation.nextRetry).toLocaleString()}.
                    </div>
                  )}
                </>
              }
            />
          )}

          <Card title="Основные настройки" size="small">
            <Space orientation="vertical" size={18} style={{ width: '100%' }}>
              <Row gutter={[12, 12]} align="middle">
                <Col>
                  <Switch
                    disabled={busy}
                    checked={status.enabled}
                    onChange={(enabled) => setStatus((prev) => ({ ...prev, enabled }))}
                  />
                </Col>
                <Col>
                  <Typography.Text strong>Блокировать рекламу и трекеры</Typography.Text>
                </Col>
                <Col flex="auto" />
                {status.lastUpdate && (
                  <Col>
                    <Tag>Обновлено: {new Date(status.lastUpdate).toLocaleString()}</Tag>
                  </Col>
                )}
              </Row>

              <div>
                <Typography.Title level={5} style={{ marginBottom: 4 }}>
                  Готовые списки
                </Typography.Title>
                <Typography.Paragraph type="secondary" style={{ marginTop: 0 }}>
                  Выберите один готовый список. Источник и расписание обновления настроятся
                  автоматически.
                </Typography.Paragraph>

                <Radio.Group
                  value={status.profile === 'custom' ? undefined : status.profile}
                  onChange={(event) => applyProfile(event.target.value)}
                  disabled={busy}
                  style={{ width: '100%' }}
                >
                  <Row gutter={[12, 12]}>
                    {status.profiles.map((profile) => (
                      <Col xs={24} lg={12} key={profile.id}>
                        <Card
                          size="small"
                          hoverable
                          onClick={() => !busy && applyProfile(profile.id)}
                          style={{
                            height: '100%',
                            borderColor:
                              status.profile === profile.id ? 'var(--ant-color-primary)' : undefined,
                          }}
                        >
                          <Radio value={profile.id}>
                            <Typography.Text strong>{profile.name}</Typography.Text>
                          </Radio>
                          <Typography.Paragraph
                            type="secondary"
                            style={{ margin: '6px 0 0 24px' }}
                          >
                            {profile.description}
                          </Typography.Paragraph>
                        </Card>
                      </Col>
                    ))}
                  </Row>
                </Radio.Group>

                {status.profile === 'custom' && (
                  <Alert
                    style={{ marginTop: 12 }}
                    type="info"
                    showIcon
                    title="Используется пользовательский набор"
                    description="Его можно изменить в разделе «Расширенные настройки»."
                  />
                )}

                {activeProfile && (
                  <Typography.Text type="secondary">
                    Автообновление: каждые {activeProfile.updateIntervalHours} ч.
                  </Typography.Text>
                )}
              </div>

              <Space wrap>
                <Button
                  type="primary"
                  icon={<SaveOutlined />}
                  loading={saving}
                  disabled={busy || !dirty}
                  onClick={() => void save()}
                >
                  Сохранить и применить
                </Button>
                <Button
                  icon={<ReloadOutlined />}
                  loading={updating}
                  disabled={busy || dirty}
                  onClick={() => void updateLists()}
                >
                  Обновить списки сейчас
                </Button>
                {paused ? (
                  <Button
                    disabled={busy || dirty}
                    onClick={() => void runAction('pause', { minutes: 0 })}
                  >
                    Возобновить
                  </Button>
                ) : (
                  <Button
                    disabled={busy || dirty || !applied.enabled}
                    onClick={() => void runAction('pause', { minutes: 15 })}
                  >
                    Пауза на 15 минут
                  </Button>
                )}
              </Space>

              {dirty && <Typography.Text type="warning">Есть несохранённые изменения.</Typography.Text>}
            </Space>
          </Card>

          <Collapse items={advancedItems} />

          <Typography.Text type="secondary">
            Готовые списки обновляются автоматически. Ручная настройка URL, клиентов и inbound
            находится в расширенном разделе и не требуется для обычного использования.
          </Typography.Text>
        </Space>
      </Spin>
    </>
  );
}
