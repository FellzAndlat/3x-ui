import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation } from 'react-router';
import {
  Alert,
  Button,
  Card,
  Col,
  ConfigProvider,
  FloatButton,
  Layout,
  Modal,
  Row,
  Space,
  Spin,
  message,
} from 'antd';
import {
  CheckOutlined,
  MoonOutlined,
  SunOutlined,
  BgColorsOutlined,
  CloudOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons';

import { HttpUtil, PromiseUtil } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import { useTheme } from '@/hooks/useTheme';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { useAllSettings } from '@/api/queries/useAllSettings';
import { AllSettingSchema } from '@/schemas/setting';
import AppSidebar from '@/layouts/AppSidebar';
import GeneralTab from './GeneralTab';
import SecurityTab from './SecurityTab';
import TelegramTab from './TelegramTab';
import EmailTab from './EmailTab';
import DiscordTab from './DiscordTab';
import SubscriptionGeneralTab from './SubscriptionGeneralTab';
import SubscriptionFormatsTab from './SubscriptionFormatsTab';
import SubscriptionBalancersTab from './SubscriptionBalancersTab';
import SwapSettingsTab from './SwapSettingsTab';
import SystemUpdateModal from './SystemUpdateModal';
import GatewayModeControl from './GatewayModeControl';
import './SettingsPage.css';

interface ApiMsg {
  success?: boolean;
}

const tabSlugs = [
  'general',
  'security',
  'telegram',
  'email',
  'discord',
  'subscription',
  'subscription-formats',
  'subscription-balancers',
];

function isIp(h: string): boolean {
  if (typeof h !== 'string') return false;
  const v4 = h.split('.');
  if (v4.length === 4 && v4.every((p) => /^\d{1,3}$/.test(p) && Number(p) <= 255)) return true;
  if (!h.includes(':') || h.includes(':::')) return false;
  const parts = h.split('::');
  if (parts.length > 2) return false;
  const split = (s: string) => (s ? s.split(':').filter(Boolean) : []);
  const head = split(parts[0]);
  const tail = split(parts[1]);
  const valid = (seg: string) => /^[0-9a-fA-F]{1,4}$/.test(seg);
  if (![...head, ...tail].every(valid)) return false;
  const groups = head.length + tail.length;
  return parts.length === 2 ? groups < 8 : groups === 8;
}

function scrollTarget() {
  return document.getElementById('content-layout') as HTMLElement;
}

export default function SettingsPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, mode, setThemeMode, lowPower, toggleLowPower, antdThemeConfig } =
    useTheme();
  const { isMobile } = useMediaQuery();
  const [modal, modalContextHolder] = Modal.useModal();
  const [messageApi, messageContextHolder] = message.useMessage();

  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);

  const {
    allSetting,
    updateSetting,
    fetched,
    spinning,
    setSpinning,
    saveDisabled,
    saveAll,
    savePayload,
  } = useAllSettings();

  const [entryHost] = useState(() => window.location.hostname);
  const [entryPort] = useState(() => window.location.port);
  const [entryIsIP] = useState(() => isIp(window.location.hostname));

  const [alertVisible, setAlertVisible] = useState(true);
  const [swapOpen, setSwapOpen] = useState(false);
  const [systemUpdateOpen, setSystemUpdateOpen] = useState(false);
  const location = useLocation();
  const slug = location.hash.replace(/^#/, '');
  const activeSlug = tabSlugs.includes(slug) ? slug : 'general';

  const activeThemeMode = lowPower ? 'dark' : mode;

  const themeOptions = [
    {
      mode: 'cyberpunk' as const,
      label: 'Cyberpunk',
      icon: <ThunderboltOutlined />,
      description: 'Неон и технологичный контраст',
    },
    {
      mode: 'light' as const,
      label: 'Светлая',
      icon: <SunOutlined />,
      description: 'Чистая и светлая',
    },
    {
      mode: 'dark' as const,
      label: 'Тёмная',
      icon: <MoonOutlined />,
      description: 'Спокойная и контрастная',
    },
    {
      mode: 'blue-gray' as const,
      label: 'Blue Gray',
      icon: <CloudOutlined />,
      description: 'Холодная тёмная',
    },
    {
      mode: 'colorful' as const,
      label: 'Colorful',
      icon: <BgColorsOutlined />,
      description: 'Более выразительная',
    },
    {
      mode: 'ultra-dark' as const,
      label: 'Ultra Dark',
      icon: <MoonOutlined />,
      description: 'Максимально тёмная',
    },
  ];

  function rebuildUrlAfterRestart(): string {
    const { webDomain, webPort, webBasePath, webCertFile, webKeyFile } = allSetting;
    const newProtocol = webCertFile || webKeyFile ? 'https:' : 'http:';

    let base = webBasePath ? webBasePath.replace(/^\//, '') : '';
    if (base && !base.endsWith('/')) base += '/';

    if (!entryIsIP) {
      const url = new URL(window.location.href);
      url.pathname = `/${base}panel/settings`;
      url.protocol = newProtocol;
      return url.toString();
    }

    let finalHost = entryHost;
    let finalPort = entryPort || '';
    if (webDomain && isIp(webDomain)) finalHost = webDomain;
    if (webPort && Number(webPort) !== Number(entryPort)) finalPort = String(webPort);

    const url = new URL(`${newProtocol}//${finalHost}`);
    if (finalPort) url.port = finalPort;
    url.pathname = `/${base}panel/settings`;
    return url.toString();
  }

  async function onSave() {
    const result = AllSettingSchema.safeParse(allSetting);
    if (!result.success) {
      const issue = result.error.issues[0];
      const fieldPath = issue?.path.join('.') ?? 'value';
      const msgKey = issue?.message ?? 'somethingWentWrong';
      messageApi.error(`${fieldPath}: ${t(msgKey, { defaultValue: msgKey })}`);
      return;
    }
    const saveResult = await saveAll();
    if (saveResult?.success) {
      window.dispatchEvent(new Event('xui-core-settings-saved'));
    }
  }

  function restartPanel() {
    modal.confirm({
      title: t('pages.settings.restartPanel'),
      content: t('pages.settings.restartPanelDesc'),
      okText: t('pages.settings.restartPanel'),
      okButtonProps: { danger: true },
      cancelText: t('cancel'),
      onOk: async () => {
        setSpinning(true);
        try {
          const msg = (await HttpUtil.post('/panel/api/setting/restartPanel')) as ApiMsg;
          if (!msg?.success) return;
          await PromiseUtil.sleep(5000);
          window.location.replace(rebuildUrlAfterRestart());
        } finally {
          setSpinning(false);
        }
      },
    });
  }

  const confAlerts = useMemo<string[]>(() => {
    const out: string[] = [];
    if (window.location.protocol !== 'https:') {
      out.push(t('pages.settings.warnHttp'));
    }
    if (allSetting.webPort === 2053) {
      out.push(t('pages.settings.warnDefaultPort'));
    }
    const segs = window.location.pathname.split('/').length < 4;
    if (segs && allSetting.webBasePath === '/') {
      out.push(t('pages.settings.warnDefaultBasePath'));
    }
    if (allSetting.subEnable) {
      let subPath = allSetting.subPath;
      if (allSetting.subURI) {
        try {
          subPath = new URL(allSetting.subURI).pathname;
        } catch {
          /* noop */
        }
      }
      if (subPath === '/sub/') {
        out.push(t('pages.settings.warnDefaultSubPath'));
      }
    }
    if (allSetting.subJsonEnable) {
      let p = allSetting.subJsonPath;
      if (allSetting.subJsonURI) {
        try {
          p = new URL(allSetting.subJsonURI).pathname;
        } catch {
          /* noop */
        }
      }
      if (p === '/json/') {
        out.push(t('pages.settings.warnDefaultJsonPath'));
      }
    }
    return out;
  }, [allSetting, t]);

  const pageClass = useMemo(() => {
    const classes = ['settings-page'];
    if (isDark) classes.push('is-dark');
    if (isUltra) classes.push('is-ultra');
    return classes.join(' ');
  }, [isDark, isUltra]);

  const categoryBody = useMemo(() => {
    switch (activeSlug) {
      case 'security':
        return (
          <SecurityTab
            allSetting={allSetting}
            updateSetting={updateSetting}
            saveSetting={savePayload}
          />
        );
      case 'telegram':
        return <TelegramTab allSetting={allSetting} updateSetting={updateSetting} />;
      case 'email':
        return <EmailTab allSetting={allSetting} updateSetting={updateSetting} />;
      case 'discord':
        return <DiscordTab allSetting={allSetting} updateSetting={updateSetting} />;
      case 'subscription':
        return <SubscriptionGeneralTab allSetting={allSetting} updateSetting={updateSetting} />;
      case 'subscription-formats':
        return <SubscriptionFormatsTab allSetting={allSetting} updateSetting={updateSetting} />;
      case 'subscription-balancers':
        return <SubscriptionBalancersTab allSetting={allSetting} updateSetting={updateSetting} />;
      default:
        return (
          <GeneralTab
            allSetting={allSetting}
            updateSetting={updateSetting}
            saveSettings={saveAll}
            onOpenSwap={() => setSwapOpen(true)}
            onOpenSystemUpdate={() => setSystemUpdateOpen(true)}
          />
        );
    }
  }, [activeSlug, allSetting, updateSetting, saveAll, savePayload]);

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {messageContextHolder}
      {modalContextHolder}
      <Layout className={pageClass}>
        <AppSidebar />

        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <Spin
              spinning={spinning || !fetched}
              delay={200}
              description={t('loading')}
              size="large"
            >
              {!fetched ? (
                <div className="loading-spacer" />
              ) : (
                <>
                  {confAlerts.length > 0 && alertVisible && (
                    <Alert
                      type="error"
                      showIcon
                      closable={{ onClose: () => setAlertVisible(false) }}
                      className="conf-alert"
                      title={t('pages.settings.securityWarnings')}
                      description={
                        <>
                          <b>{t('pages.settings.panelExposed')}</b>
                          <ul>
                            {confAlerts.map((msg, i) => (
                              <li key={i}>{msg}</li>
                            ))}
                          </ul>
                        </>
                      }
                    />
                  )}

                  <Row gutter={[isMobile ? 8 : 16, isMobile ? 0 : 12]}>
                    <Col span={24}>
                      <Card hoverable>
                        <Row className="header-row">
                          <Col xs={24} sm={10} className="header-actions">
                            <Space wrap>
                              <Button type="primary" disabled={saveDisabled} onClick={onSave}>
                                {t('pages.settings.save')}
                              </Button>
                              <Button
                                type="primary"
                                danger
                                disabled={!saveDisabled}
                                onClick={restartPanel}
                              >
                                {t('pages.settings.restartPanel')}
                              </Button>
                              <GatewayModeControl />
                            </Space>
                          </Col>
                          <Col xs={24} sm={14} className="header-info">
                            <FloatButton.BackTop target={scrollTarget} visibilityHeight={200} />
                            <Alert type="warning" showIcon title={t('pages.settings.infoDesc')} />
                          </Col>
                        </Row>
                      </Card>
                    </Col>

                    <Col span={24}>
                      <Card hoverable>{categoryBody}</Card>
                    </Col>

                    {activeSlug === 'general' && (slug === '' || slug === 'general') && (
                      <Col span={24}>
                        <Card className="theme-picker-card">
                          <div className="theme-picker-header">
                            <div>
                              <div className="theme-picker-title">Оформление</div>
                              <div className="theme-picker-subtitle">
                                Выберите внешний вид панели
                              </div>
                            </div>
                          </div>
                          <div
                            className="theme-picker-grid"
                            role="radiogroup"
                            aria-label="Тема панели"
                          >
                            {themeOptions.map((option) => {
                              const selected = activeThemeMode === option.mode;
                              const unavailable = lowPower && option.mode === 'cyberpunk';
                              return (
                                <button
                                  key={option.mode}
                                  type="button"
                                  className={`theme-picker-option theme-${option.mode} ${selected ? 'is-selected' : ''} ${unavailable ? 'is-unavailable' : ''}`}
                                  aria-checked={selected}
                                  role="radio"
                                  aria-disabled={unavailable}
                                  disabled={unavailable}
                                  onClick={() => setThemeMode(option.mode)}
                                >
                                  <span className="theme-picker-preview" aria-hidden="true">
                                    <span className="theme-preview-sidebar" />
                                    <span className="theme-preview-content">
                                      <span />
                                      <span />
                                      <span />
                                    </span>
                                  </span>
                                  <span className="theme-picker-meta">
                                    <span className="theme-picker-name">
                                      {option.icon} {option.label}
                                    </span>
                                    <span className="theme-picker-description">
                                      {option.description}
                                    </span>
                                  </span>
                                  <span className="theme-picker-check">
                                    {selected && <CheckOutlined />}
                                  </span>
                                </button>
                              );
                            })}
                          </div>
                          <div className="theme-picker-footer">
                            <Button
                              className={`low-power-toggle ${lowPower ? 'is-active' : ''}`}
                              onClick={toggleLowPower}
                              aria-pressed={lowPower}
                            >
                              <span className="low-power-toggle-icon" aria-hidden="true">
                                <ThunderboltOutlined />
                              </span>
                              <span className="low-power-toggle-copy">
                                <span className="low-power-toggle-title">
                                  {lowPower ? 'Режим слабого ПК включён' : 'Режим слабого ПК'}
                                </span>
                                <span className="low-power-toggle-description">
                                  {lowPower
                                    ? 'Cyberpunk временно отключён'
                                    : 'Отключает тяжёлые эффекты'}
                                </span>
                              </span>
                            </Button>
                          </div>
                        </Card>
                      </Col>
                    )}
                  </Row>
                </>
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>
      <Modal
        open={swapOpen}
        title={t('pages.settings.swap.menu')}
        width={isMobile ? 'calc(100vw - 16px)' : 'min(1360px, calc(100vw - 32px))'}
        rootClassName="swap-settings-modal"
        destroyOnClose
        footer={null}
        styles={{
          body: {
            maxHeight: 'calc(100vh - 160px)',
            overflowY: 'auto',
            overflowX: 'hidden',
          },
        }}
        onCancel={() => setSwapOpen(false)}
      >
        <SwapSettingsTab />
      </Modal>
      <SystemUpdateModal open={systemUpdateOpen} onClose={() => setSystemUpdateOpen(false)} />
    </ConfigProvider>
  );
}
