import { useEffect, useState } from 'react';
import {
  Alert,
  AutoComplete,
  Button,
  Card,
  Col,
  Input,
  InputNumber,
  Row,
  Space,
  Select,
  Spin,
  Switch,
  Tag,
  Table,
  Typography,
  message,
} from 'antd';
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { onNumber } from '@/utils/onNumber';

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
type AdBlockDomainCheck = {
  policy: string;
  profile: string;
  domain: string;
  blocked: boolean;
  reason: string;
  rule: string;
  sources: string[];
  contextRequired: boolean;
  application: AdBlockStatus['application'];
};
const defaultServer = {
  enabled: false,
  outbound: '',
  limits: { maxConnections: 128, workers: 2, bodyMiB: 8, queueMs: 250 },
};
type AdBlockStatus = {
  server?: typeof defaultServer;
  serverOutbounds?: string[];
  serverRuntime?: {
    listen?: string;
    running: boolean;
    requests: number;
    filtered: number;
    busy: number;
    tlsFailures: number;
    upstreamFailures: number;
    unchanged: number;
    malformed?: number;
    oversized?: number;
    unsupported?: number;
    lastError: string;
  };
  serverCertificate?: { fingerprint: string; expires: string };
  policies: AdBlockPolicy[];
  scope: AdBlockScope;
  scopeOptions: {
    inbounds: { value: string; label: string }[];
    clients: { value: string; label: string }[];
  };
  application: { pending: boolean; lastError: string; appliedAt: string; nextRetry: string };
  pausedUntil: string;
  sourceStatuses: {
    url: string;
    domainCount: number;
    updatedAt: string;
    checkedAt: string;
    lastError: string;
    stale: boolean;
  }[];
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
  youtubeVideoAdsSupported: boolean;
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

const emptyStatus: AdBlockStatus = {
  policies: [],
  scope: { inboundMode: 'all', inbounds: [], clientMode: 'all', clients: [] },
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
  youtubeVideoAdsSupported: false,
  automation: { lastAttempt: '', lastError: '', retryCount: 0, nextRetry: '' },
};

function settingsValue(status: AdBlockStatus) {
  return {
    server: status.server || defaultServer,
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

export default function AdBlockTab() {
  const [status, setStatus] = useState<AdBlockStatus>(emptyStatus);
  const [applied, setApplied] = useState<AdBlockStatus>(emptyStatus);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [loadAttempt, setLoadAttempt] = useState(0);
  const [saving, setSaving] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [acting, setActing] = useState(false);
  const [domain, setDomain] = useState('');
  const [checkInbound, setCheckInbound] = useState<string>();
  const [checkClient, setCheckClient] = useState<string>();
  const [checking, setChecking] = useState(false);
  const [checkResult, setCheckResult] = useState<AdBlockDomainCheck>();
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
        const current = await HttpUtil.get<AdBlockStatus>('/panel/api/adblock/status', undefined, {
          silent: true,
        });
        if (current.success && current.obj) {
          setStatus(current.obj);
          setApplied(current.obj);
        }
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

  const applyProfile = (id: string) => {
    const profile = status.profiles.find((preset) => preset.id === id);
    setStatus((prev) =>
      profile
        ? {
            ...prev,
            profile: id,
            sources: profile.sources,
            autoUpdate: true,
            updateIntervalHours: profile.updateIntervalHours,
            youtubeMode: prev.youtubeMode === 'off' ? 'compatible' : prev.youtubeMode,
          }
        : { ...prev, profile: 'custom' },
    );
  };

  const busy = loading || loadFailed || saving || updating || acting;
  const dirty = JSON.stringify(settingsValue(status)) !== JSON.stringify(settingsValue(applied));
  const paused = !!applied.pausedUntil && new Date(applied.pausedUntil).getTime() > now;
  const refreshStatus = async () => {
    const response = await HttpUtil.get<AdBlockStatus>('/panel/api/adblock/status', undefined, {
      silent: true,
    });
    if (response.success && response.obj) {
      setApplied(response.obj);
      setStatus((prev) => ({ ...response.obj!, ...settingsValue(prev) }));
    }
  };
  useEffect(() => {
    if (loading || busy) return;
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
  }, [dirty, loading, busy]);
  const runAction = async (path: string, body?: { minutes: number }) => {
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
      messageApi.success(
        path === 'apply'
          ? 'Настройки применены'
          : body?.minutes
            ? 'Фильтрация приостановлена'
            : 'Фильтрация возобновлена',
      );
    } catch (error) {
      await refreshStatus().catch(() => {});
      messageApi.error(error instanceof Error ? error.message : 'Ошибка применения');
    } finally {
      setActing(false);
    }
  };
  const checkDomain = async () => {
    setChecking(true);
    try {
      const response = (await HttpUtil.post(
        '/panel/api/adblock/check',
        { domain, inbound: checkInbound || '', client: checkClient || '' },
        { silent: true },
      )) as ApiMsg<AdBlockDomainCheck>;
      if (response.success && response.obj) setCheckResult(response.obj);
      else {
        setCheckResult(undefined);
        messageApi.error(response.msg || 'Ошибка проверки домена');
      }
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : 'Ошибка проверки');
    } finally {
      setChecking(false);
    }
  };

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
              Повторить загрузку
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
              Серверная блокировка рекламных и трекер-доменов для трафика через Xray и sing-box.
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
                ? 'Настройки сохранены — ожидают применения'
                : paused
                  ? 'AdBlock временно приостановлен'
                  : applied.enabled
                    ? 'AdBlock включён'
                    : 'AdBlock выключен'
            }
            description={
              applied.application.pending
                ? applied.application.lastError || 'Ожидается применение к ядру.'
                : paused
                  ? `Автоматическое возобновление: ${new Date(applied.pausedUntil).toLocaleString()}.`
                  : applied.enabled
                    ? `Доменов в профилях: ${applied.domainCount.toLocaleString()}. Источников: ${applied.sourceCount}.`
                    : 'Включите фильтрацию, настройте источники и сохраните изменения.'
            }
          />

          <Space wrap>
            {applied.application.pending && (
              <Button
                loading={acting}
                disabled={busy || dirty}
                onClick={() => void runAction('apply')}
              >
                Повторить применение
              </Button>
            )}
            {paused ? (
              <Button
                disabled={busy || dirty}
                onClick={() => void runAction('pause', { minutes: 0 })}
              >
                Возобновить сейчас
              </Button>
            ) : (
              [5, 15, 60].map((minutes) => (
                <Button
                  key={minutes}
                  disabled={busy || dirty || !applied.enabled}
                  onClick={() => void runAction('pause', { minutes })}
                >
                  Пауза на {minutes} мин
                </Button>
              ))
            )}
          </Space>
          {applied.application.nextRetry && applied.application.pending && (
            <Typography.Text type="secondary">
              Автоматическая попытка применения после{' '}
              {new Date(applied.application.nextRetry).toLocaleString()}.
            </Typography.Text>
          )}

          <div>
            <Typography.Text strong>Общий профиль</Typography.Text>
            <Select
              aria-label="Автоматический профиль"
              disabled={busy}
              value={status.profile}
              style={{ width: '100%', marginTop: 8 }}
              options={[
                ...status.profiles.map((profile) => ({ value: profile.id, label: profile.name })),
                { value: 'custom', label: 'Свой профиль' },
              ]}
              onChange={applyProfile}
            />
            <Typography.Paragraph type="secondary" style={{ marginTop: 8 }}>
              {status.profiles.find((profile) => profile.id === status.profile)?.description ||
                'Настройте источники и расписание вручную.'}{' '}
              Выбор профиля настраивает источники и автообновление. Применяется после сохранения.
            </Typography.Paragraph>
          </div>

          <div>
            <Typography.Text strong>Совместимость с YouTube</Typography.Text>
            <Select
              aria-label="Режим YouTube"
              disabled={busy}
              value={status.youtubeMode}
              style={{ width: '100%', marginTop: 8 }}
              options={[
                { value: 'off', label: 'Общие правила без исключений YouTube' },
                { value: 'compatible', label: 'Совместимость: сохранять воспроизведение' },
                { value: 'privacy', label: 'Совместимость + отдельные рекламные домены Google' },
              ]}
              onChange={(youtubeMode) => setStatus((prev) => ({ ...prev, youtubeMode }))}
            />
            <Alert
              style={{ marginTop: 12 }}
              type="info"
              showIcon
              title="Видеореклама YouTube требует клиентской фильтрации"
              description={
                <>
                  В режиме совместимости автоматически исключаются общие домены видео, API и
                  изображений. Режим отдельных рекламных доменов блокирует сопутствующие запросы, но
                  не видеовставки. В браузере нужен блокировщик содержимого; в официальном
                  приложении YouTube сервер не может надёжно отличить рекламу от видео.{' '}
                  <Typography.Link
                    href="https://adguard.com/en/article/how-to-block-ads-on-youtube.html"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    Способы фильтрации на устройстве
                  </Typography.Link>
                </>
              }
            />
          </div>

          <Alert
            type="info"
            showIcon
            title="Клиентская фильтрация YouTube"
            description={
              <>
                <p>
                  Для Chrome, Edge и Chromium доступно экспериментальное расширение: очистка
                  рекламных данных плеера, скрытие рекламных элементов и пропуск видео, явно
                  отмеченных как реклама. Оно работает независимо от серверного профиля.
                </p>
                <Button
                  href={`${(window.X_UI_BASE_PATH || '/').replace(/\/?$/, '/')}panel/api/adblock/youtube-extension`}
                >
                  Скачать расширение YouTube
                </Button>
                <p>
                  Распакуйте ZIP, откройте chrome://extensions (Edge: edge://extensions), включите
                  режим разработчика и загрузите распакованную папку с manifest.json. Обновите
                  страницу YouTube.
                </p>
                <p>
                  Сертификат и пароль панели не нужны. В официальном Android/iOS-приложении
                  расширение не работает; изменения плеера YouTube могут потребовать обновления
                  расширения.
                </p>
              </>
            }
          />

          <Card title="Серверная фильтрация YouTube (экспериментальная)">
            <Space direction="vertical" style={{ width: '100%' }}>
              <Space>
                <Switch
                  aria-label="Серверный фильтр YouTube"
                  disabled={loading || loadFailed}
                  checked={(status.server || defaultServer).enabled}
                  onChange={(enabled) =>
                    setStatus((prev) => ({
                      ...prev,
                      server: { ...(prev.server || defaultServer), enabled },
                    }))
                  }
                />
                <span>Управлять прокси автоматически вместе с AdBlock</span>
                <Tag color={applied.serverRuntime?.running ? 'green' : 'default'}>
                  {applied.serverRuntime?.running ? 'Прокси запущен' : 'Прокси остановлен'}
                </Tag>
              </Space>
              <Alert
                type="info"
                showIcon
                title="HTTPS-фильтрация требует доверия сертификату на участвующих устройствах"
                description="Маршруты создаются автоматически по общей области действия и профилям. Режим «без фильтрации», выключение и пауза AdBlock отключают перехват. Приватный ключ остаётся на сервере. Официальные приложения могут отклонять сертификат; реклама внутри видеопотока не удаляется."
              />
              <Space wrap>
                <Button
                  href={`${(window.X_UI_BASE_PATH || '/').replace(/\/?$/, '/')}panel/api/adblock/youtube-server-certificate`}
                >
                  Скачать публичный CA
                </Button>
                <a
                  href="https://github.com/SawaMEN/3x-ui/blob/AdBlock/docs/youtube-server-filter.md"
                  target="_blank"
                  rel="noreferrer"
                >
                  Установка и проверка сертификата
                </a>
              </Space>
              {applied.serverCertificate?.fingerprint && (
                <Typography.Paragraph copyable>
                  SHA-256: {applied.serverCertificate.fingerprint}
                </Typography.Paragraph>
              )}
              {applied.serverCertificate?.expires && (
                <span>
                  Сертификат до: {new Date(applied.serverCertificate.expires).toLocaleDateString()}
                </span>
              )}
              <Row gutter={[16, 12]}>
                <Col xs={24} md={12}>
                  <Typography.Text>Исходящий outbound / балансировщик</Typography.Text>
                  <AutoComplete
                    options={(applied.serverOutbounds || []).map((value) => ({ value }))}
                    style={{ width: '100%' }}
                    placeholder="Пусто — напрямую; тег outbound можно ввести вручную"
                    value={(status.server || defaultServer).outbound}
                    onChange={(value) =>
                      setStatus((prev) => ({
                        ...prev,
                        server: { ...(prev.server || defaultServer), outbound: value },
                      }))
                    }
                  />
                </Col>
                {(
                  [
                    { key: 'maxConnections', label: 'Соединений', min: 8, max: 512 },
                    { key: 'workers', label: 'Обработчиков', min: 1, max: 8 },
                    { key: 'bodyMiB', label: 'Ответ, МиБ', min: 1, max: 16 },
                    { key: 'queueMs', label: 'Ожидание очереди, мс', min: 0, max: 2000 },
                  ] as const
                ).map((item) => (
                  <Col key={item.key} xs={12} md={6}>
                    <Typography.Text>{item.label}</Typography.Text>
                    <InputNumber
                      min={item.min}
                      max={item.max}
                      value={(status.server || defaultServer).limits[item.key]}
                      onChange={(value) => {
                        if (value !== null)
                          setStatus((prev) => {
                            const server = prev.server || defaultServer;
                            return {
                              ...prev,
                              server: {
                                ...server,
                                limits: { ...server.limits, [item.key]: value },
                              },
                            };
                          });
                      }}
                    />
                  </Col>
                ))}
              </Row>
              <Typography.Text type="secondary">
                Обработчиков × лимит ответа ≤ 32 МиБ; декодирование и разбор требуют дополнительной
                памяти. Новый прокси запускается до применения маршрутов; при ошибке настройки
                откатываются.
              </Typography.Text>
              {applied.serverRuntime?.listen && (
                <Typography.Paragraph copyable>
                  Локальный адрес: {applied.serverRuntime.listen}
                </Typography.Paragraph>
              )}
              {applied.serverRuntime && (
                <Space wrap>
                  <Tag>Запросов: {applied.serverRuntime.requests}</Tag>
                  <Tag>Очищено ответов: {applied.serverRuntime.filtered}</Tag>
                  <Tag>Очередь переполнена: {applied.serverRuntime.busy}</Tag>
                  <Tag>Ошибки TLS клиента: {applied.serverRuntime.tlsFailures}</Tag>
                  <Tag>Ошибки upstream: {applied.serverRuntime.upstreamFailures}</Tag>
                  <Tag>Повреждённые ответы: {applied.serverRuntime.malformed || 0}</Tag>
                  <Tag>Слишком большие: {applied.serverRuntime.oversized || 0}</Tag>
                  <Tag>Неподдерживаемое сжатие: {applied.serverRuntime.unsupported || 0}</Tag>
                </Space>
              )}
              {applied.serverRuntime?.lastError && (
                <Alert type="warning" title={applied.serverRuntime.lastError} />
              )}
              <Typography.Text type="secondary">
                Проверка воспроизведения: после доверия CA откройте обычное видео, Shorts и
                трансляцию; проверьте перемотку и субтитры. Счётчик очищенных ответов подтверждает
                обработку данных, а не отсутствие всей рекламы.
              </Typography.Text>
            </Space>
          </Card>

          {applied.automation.lastError && (
            <Alert
              type="warning"
              showIcon
              title="Есть проблемы с обновлением источников — используются рабочие копии"
              description={
                <>
                  {applied.automation.lastError}
                  {applied.enabled && applied.autoUpdate && applied.automation.nextRetry && (
                    <div>
                      Повтор после: {new Date(applied.automation.nextRetry).toLocaleString()}.
                      Неудачных попыток: {applied.automation.retryCount}.
                    </div>
                  )}
                </>
              }
            />
          )}

          <Row gutter={[16, 16]} align="middle">
            <Col>
              <Switch
                disabled={busy}
                checked={status.enabled}
                onChange={(enabled) => setStatus((prev) => ({ ...prev, enabled }))}
              />
            </Col>
            <Col>
              <Typography.Text strong>Фильтрация рекламы</Typography.Text>
            </Col>
            <Col flex="auto" />
            <Col>
              <Tag>{status.domainCount.toLocaleString()} доменов</Tag>
            </Col>
            {status.lastUpdate && (
              <Col>
                <Tag>Обновлено: {status.lastUpdate}</Tag>
              </Col>
            )}
          </Row>

          <Row gutter={[16, 12]} align="middle">
            <Col>
              <Switch
                disabled={busy}
                checked={status.autoUpdate}
                onChange={(autoUpdate) => setStatus((prev) => ({ ...prev, autoUpdate }))}
              />
            </Col>
            <Col>
              <Typography.Text strong>Автоматически обновлять списки</Typography.Text>
            </Col>
            <Col>
              <InputNumber
                min={1}
                max={168}
                precision={0}
                value={status.updateIntervalHours}
                disabled={busy || !status.autoUpdate}
                suffix="ч"
                onChange={onNumber((value) =>
                  setStatus((prev) => ({ ...prev, updateIntervalHours: value })),
                )}
              />
            </Col>
            <Col>
              <Typography.Text type="secondary">
                Интервал: 1–168 часов. При ошибке — автоматический повтор через 5 минут с
                увеличением задержки до 6 часов.
              </Typography.Text>
            </Col>
          </Row>

          <div>
            <Typography.Text strong>Область фильтрации</Typography.Text>
            <Typography.Paragraph type="secondary">
              Это общая область действия всех профилей. Условия по inbound и клиентам применяются
              вместе. Исключения отключают только AdBlock; остальные правила маршрутизации
              сохраняются. Для подключения без идентификатора клиента используйте выбор inbound.
            </Typography.Paragraph>
            <Row gutter={[16, 12]}>
              {(['inbound', 'client'] as const).map((kind) => {
                const modeKey = kind === 'inbound' ? 'inboundMode' : 'clientMode';
                const itemsKey = kind === 'inbound' ? 'inbounds' : 'clients';
                return (
                  <Col xs={24} md={12} key={kind}>
                    <Typography.Text>
                      {kind === 'inbound' ? 'Входящие подключения' : 'Клиенты (email / имя)'}
                    </Typography.Text>
                    <Select
                      style={{ width: '100%', marginTop: 8 }}
                      disabled={busy}
                      value={status.scope[modeKey]}
                      options={[
                        { value: 'all', label: 'Все' },
                        { value: 'include', label: 'Только выбранные' },
                        { value: 'exclude', label: 'Все, кроме выбранных' },
                      ]}
                      onChange={(mode) =>
                        setStatus((prev) => ({
                          ...prev,
                          scope: {
                            ...prev.scope,
                            [modeKey]: mode,
                            [itemsKey]: mode === 'all' ? [] : prev.scope[itemsKey],
                          },
                        }))
                      }
                    />
                    {status.scope[modeKey] !== 'all' && (
                      <Select
                        mode="tags"
                        style={{ width: '100%', marginTop: 8 }}
                        disabled={busy}
                        value={status.scope[itemsKey]}
                        options={status.scopeOptions[itemsKey]}
                        placeholder="Выберите или введите идентификатор"
                        onChange={(items) =>
                          setStatus((prev) => ({
                            ...prev,
                            scope: { ...prev.scope, [itemsKey]: items },
                          }))
                        }
                      />
                    )}
                    {status.scope[modeKey] === 'include' && status.scope[itemsKey].length === 0 && (
                      <Typography.Text type="warning">
                        Ничего не выбрано — фильтрация в этой области не применяется.
                      </Typography.Text>
                    )}
                  </Col>
                );
              })}
            </Row>
          </div>
          <div>
            <Typography.Text strong>Отдельные профили для клиентов и inbound</Typography.Text>
            <Typography.Paragraph type="secondary">
              Первое включённое правило с совпадающими условиями выбирает профиль. Если совпадений
              нет, используется общий профиль. «Без фильтрации» отключает только AdBlock для
              выбранного подключения. Область фильтрации выше, разрешённые домены, дополнительные
              домены и пауза действуют для всех профилей.
            </Typography.Paragraph>
            <Space orientation="vertical" style={{ width: '100%' }}>
              {status.policies.map((policy, index) => {
                const patch = (update: Partial<AdBlockPolicy>) =>
                  setStatus((prev) => ({
                    ...prev,
                    policies: prev.policies.map((item) =>
                      item.id === policy.id ? { ...item, ...update } : item,
                    ),
                  }));
                const move = (delta: number) =>
                  setStatus((prev) => {
                    const next = [...prev.policies];
                    [next[index], next[index + delta]] = [next[index + delta], next[index]];
                    return { ...prev, policies: next };
                  });
                return (
                  <Card
                    size="small"
                    key={policy.id}
                    title={`${index + 1}. ${policy.name}`}
                    extra={
                      <Space>
                        <Button
                          size="small"
                          disabled={busy || index === 0}
                          onClick={() => move(-1)}
                        >
                          Выше
                        </Button>
                        <Button
                          size="small"
                          disabled={busy || index === status.policies.length - 1}
                          onClick={() => move(1)}
                        >
                          Ниже
                        </Button>
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
                      </Space>
                    }
                  >
                    <Space wrap style={{ marginBottom: 12 }}>
                      <Switch
                        aria-label={`Включить правило ${index + 1}`}
                        disabled={busy}
                        checked={policy.enabled}
                        onChange={(enabled) => patch({ enabled })}
                      />
                      <Input
                        aria-label={`Название правила ${index + 1}`}
                        maxLength={64}
                        disabled={busy}
                        value={policy.name}
                        onChange={(event) => patch({ name: event.target.value })}
                      />
                      <Select
                        aria-label={`Профиль правила ${index + 1}`}
                        style={{ minWidth: 220 }}
                        disabled={busy}
                        value={policy.profile}
                        options={[
                          ...status.profiles.map((profile) => ({
                            value: profile.id,
                            label: profile.name,
                          })),
                          { value: 'off', label: 'Без фильтрации' },
                        ]}
                        onChange={(profile) => patch({ profile })}
                      />
                    </Space>
                    <Row gutter={[16, 12]}>
                      {(['inbound', 'client'] as const).map((kind) => {
                        const modeKey = kind === 'inbound' ? 'inboundMode' : 'clientMode';
                        const itemsKey = kind === 'inbound' ? 'inbounds' : 'clients';
                        return (
                          <Col xs={24} md={12} key={kind}>
                            <Typography.Text>
                              {kind === 'inbound' ? 'Inbound' : 'Клиенты'}
                            </Typography.Text>
                            <Select
                              aria-label={`${kind} правила ${index + 1}`}
                              disabled={busy}
                              style={{ width: '100%', marginTop: 8 }}
                              value={policy.scope[modeKey]}
                              options={[
                                { value: 'all', label: 'Все' },
                                { value: 'include', label: 'Только выбранные' },
                                { value: 'exclude', label: 'Все, кроме выбранных' },
                              ]}
                              onChange={(mode) =>
                                patch({
                                  scope: {
                                    ...policy.scope,
                                    [modeKey]: mode,
                                    [itemsKey]: mode === 'all' ? [] : policy.scope[itemsKey],
                                  },
                                })
                              }
                            />
                            {policy.scope[modeKey] !== 'all' && (
                              <Select
                                mode="tags"
                                disabled={busy}
                                style={{ width: '100%', marginTop: 8 }}
                                value={policy.scope[itemsKey]}
                                options={status.scopeOptions[itemsKey]}
                                placeholder="Выберите или введите идентификатор"
                                onChange={(items) =>
                                  patch({ scope: { ...policy.scope, [itemsKey]: items } })
                                }
                              />
                            )}
                            {policy.scope[modeKey] === 'include' &&
                              policy.scope[itemsKey].length === 0 && (
                                <Typography.Text type="warning">
                                  Выберите подключение — пустое правило не применяется.
                                </Typography.Text>
                              )}
                          </Col>
                        );
                      })}
                    </Row>
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
                        name: `Профиль ${prev.policies.length + 1}`,
                        enabled: true,
                        profile: 'balanced',
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
                Добавить профиль подключения
              </Button>
            </Space>
          </div>

          <div>
            <Typography.Text strong>Проверка домена</Typography.Text>
            <Typography.Paragraph type="secondary">
              Проверяет сохранённые настройки AdBlock. Доступность сайта и другие правила
              маршрутизации не проверяются.
            </Typography.Paragraph>
            <Space wrap style={{ width: '100%', marginBottom: 8 }}>
              <AutoComplete
                allowClear
                style={{ width: 240 }}
                value={checkInbound || ''}
                options={applied.scopeOptions.inbounds}
                placeholder="Inbound для проверки"
                onChange={(value) => {
                  setCheckInbound(value);
                  setCheckResult(undefined);
                }}
              />
              <AutoComplete
                allowClear
                style={{ width: 240 }}
                value={checkClient || ''}
                options={applied.scopeOptions.clients}
                placeholder="Клиент для проверки"
                onChange={(value) => {
                  setCheckClient(value);
                  setCheckResult(undefined);
                }}
              />
            </Space>
            <Input.Search
              value={domain}
              placeholder="ads.example.com"
              enterButton="Проверить"
              loading={checking}
              disabled={loading || checking}
              onChange={(event) => {
                setDomain(event.target.value);
                setCheckResult(undefined);
              }}
              onSearch={() => void checkDomain()}
            />
            {checkResult && (
              <Alert
                style={{ marginTop: 12 }}
                showIcon
                type={
                  checkResult.contextRequired || checkResult.application.pending
                    ? 'warning'
                    : checkResult.blocked
                      ? 'error'
                      : 'success'
                }
                title={`${checkResult.domain}: ${checkResult.reason}`}
                description={
                  <>
                    {checkResult.policy && (
                      <div>
                        Профиль подключения: {checkResult.policy} ({checkResult.profile})
                      </div>
                    )}
                    {checkResult.rule && <div>Правило: {checkResult.rule}</div>}
                    {checkResult.sources.map((source) => (
                      <div key={source}>Источник: {source}</div>
                    ))}
                    {checkResult.application.pending && (
                      <div>
                        Настройки ещё не применены к ядру; текущее поведение может отличаться.
                      </div>
                    )}
                  </>
                }
              />
            )}
          </div>

          <div>
            <Typography.Text strong>Источники списков</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
              Один публичный HTTP/HTTPS URL на строку, максимум 32 источника. Поддерживаются
              hosts-файлы и списки доменов (точное совпадение), ||domain^ и domain:domain (включая
              поддомены). Международные домены указывайте в Punycode.
            </Typography.Paragraph>
            <Input.TextArea
              disabled={busy}
              value={status.sources}
              rows={6}
              placeholder={'https://example.org/hosts.txt\nhttps://example.org/domains.txt'}
              onChange={(event) =>
                setStatus((prev) => ({ ...prev, sources: event.target.value, profile: 'custom' }))
              }
            />
          </div>

          {applied.sourceStatuses.length > 0 && (
            <Table
              size="small"
              rowKey="url"
              pagination={false}
              scroll={{ x: 750 }}
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
                  title: 'Рабочая копия',
                  dataIndex: 'updatedAt',
                  render: (value: string) =>
                    value ? new Date(value).toLocaleString() : 'Не загружена',
                },
                {
                  title: 'Проверен',
                  dataIndex: 'checkedAt',
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
          )}

          <div>
            <Typography.Text strong>Дополнительная блокировка</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
              Домены, которые нужно блокировать независимо от внешних источников. Один домен на
              строку. Для блокировки всех поддоменов используйте domain:example.com.
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
          </div>

          <div>
            <Typography.Text strong>Разрешённые домены</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ marginBottom: 8 }}>
              Исключение разрешает домен и его поддомены только для AdBlock; остальные правила
              маршрутизации продолжают работать. Если широкое domain:-правило охватывает разрешённый
              поддомен, оно исключается целиком.
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
          </div>

          {dirty && (
            <Typography.Text type="warning">
              Есть несохранённые изменения. Сохраните их перед обновлением списков.
            </Typography.Text>
          )}
          <Space wrap>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={saving}
              disabled={busy || !dirty}
              onClick={() => void save()}
            >
              Сохранить
            </Button>
            <Button
              icon={<ReloadOutlined />}
              loading={updating}
              disabled={busy || dirty}
              onClick={() => void updateLists()}
            >
              Обновить списки
            </Button>
          </Space>

          <Alert
            type="warning"
            showIcon
            title="Ограничения серверной фильтрации"
            description="Фильтрация работает для трафика, проходящего через ядро, по домену назначения или HTTP Host/TLS SNI/QUIC. Соединения с IP без доступного имени (в том числе скрытого ECH) могут обходить фильтр. Рекламу, которая отдаётся с того же домена и по тому же HTTPS-соединению, что и основной контент, без MITM надёжно удалить нельзя."
          />
        </Space>
      </Spin>
    </>
  );
}
