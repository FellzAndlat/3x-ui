import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Layout,
  Space,
  Tabs,
  Typography,
  message,
} from 'antd';
import type { TabsProps } from 'antd';
import {
  ApiOutlined,
  AppstoreOutlined,
  ClockCircleOutlined,
  CustomerServiceOutlined,
  LinkOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons';

import { ClipboardManager, LanguageManager } from '@/utils';
import { setMessageInstance } from '@/utils/messageBus';
import { useTheme } from '@/hooks/useTheme';
import SubAppsTab from './SubAppsTab';
import SubConfigsTab from './SubConfigsTab';
import SubHeader from './SubHeader';
import SubHero from './SubHero';
import SubLinksTab from './SubLinksTab';
import {
  buildSubApps,
  buildTelemtEndpoint,
  daysUntil,
  detectPlatform,
  resolveSubStatus,
} from './subPageModel';
import './SubPage.css';

const subData = window.__SUB_PAGE_DATA__ || {};

const sId = subData.sId || '';
const subUrl = subData.subUrl || '';
const subJsonUrl = subData.subJsonUrl || '';
const subClashUrl = subData.subClashUrl || '';
const subTitle = subData.subTitle || '';
const subSupportUrl = subData.subSupportUrl || '';
const updateHours = Number(subData.subUpdates || 0);
const announce = subData.announce || '';
const links: string[] = Array.isArray(subData.links) ? subData.links : [];
const linkEmails: string[] = Array.isArray(subData.emails) ? subData.emails : [];
const totalByte = Number(subData.totalByte || 0);
const usedByte =
  Number(subData.usedByte || 0) ||
  Number(subData.downloadByte || 0) + Number(subData.uploadByte || 0);
const expireMs = Number(subData.expire || 0) * 1000;
const clientEmail = [...new Set(linkEmails.filter(Boolean))].join(', ');
const loadedAt = Date.now();

type TelemtProxyProfile = {
  host: string;
  port: number;
  tls: boolean;
  link: string;
};

type TelemtSubscriptionData = {
  enabled?: boolean;
  personal?: TelemtProxyProfile;
  webProxyEnabled?: boolean;
  webProxy?: string;
};

const heroData = {
  status: resolveSubStatus({ enabled: !!subData.enabled, usedByte, totalByte, expireMs }, loadedAt),
  daysLeft: daysUntil(expireMs, loadedAt),
  usedByte,
  totalByte,
  expireMs,
  lastOnlineMs: Number(subData.lastOnline || 0),
  download: subData.download || '0',
  upload: subData.upload || '0',
  used: subData.used || '0',
  total: subData.total || '∞',
  remained: subData.remained || '',
  datepicker: subData.datepicker || 'gregorian',
};

const apps = buildSubApps({ subUrl, sId, subTitle });
const initialPlatform = detectPlatform(navigator.userAgent);
const RTL_LANGUAGES = new Set(['fa-IR', 'ar-EG']);

// The sub page runs its own violet accent, so every antd control on it picks the
// hue up instead of the panel blue useTheme pins. Mirrored in SubPage.css.
const ACCENT = {
  light: {
    primary: '#7c3aed',
    hover: '#8b5cf6',
    active: '#6d28d9',
    rail: 'rgba(124, 58, 237, 0.16)',
  },
  dark: {
    primary: '#a78bfa',
    hover: '#c4b5fd',
    active: '#8b5cf6',
    rail: 'rgba(167, 139, 250, 0.18)',
  },
};

export default function SubPage() {
  const { t } = useTranslation();
  const { isDark, isUltra, antdThemeConfig } = useTheme();
  const [messageApi, messageContextHolder] = message.useMessage();
  useEffect(() => {
    setMessageInstance(messageApi);
  }, [messageApi]);
  const [lang, setLang] = useState<string>(() => LanguageManager.getLanguage('subscription'));
  const [telemtData, setTelemtData] = useState<TelemtSubscriptionData>({});
  const [telemtLoading, setTelemtLoading] = useState(true);
  const [telemtError, setTelemtError] = useState(false);

  useEffect(() => {
    const endpoint = buildTelemtEndpoint(window.location.href, sId);
    if (!endpoint) {
      setTelemtLoading(false);
      setTelemtError(true);
      return;
    }

    const controller = new AbortController();
    setTelemtLoading(true);
    setTelemtError(false);

    void fetch(endpoint, {
      method: 'GET',
      cache: 'no-store',
      credentials: 'same-origin',
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`Telemt subscription request failed with ${response.status}`);
        }
        return (await response.json()) as TelemtSubscriptionData;
      })
      .then((data) => {
        setTelemtData(data);
        setTelemtError(false);
      })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError')) {
          setTelemtData({});
          setTelemtError(true);
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setTelemtLoading(false);
        }
      });

    return () => controller.abort();
  }, []);

  const onLangChange = useCallback((next: string) => {
    setLang(next);
    LanguageManager.setLanguage(next, 'subscription');
  }, []);

  const copy = useCallback(
    async (value: string, toast?: string) => {
      if (!value) return;
      const ok = await ClipboardManager.copyText(value);
      if (ok) messageApi.success(toast ?? t('copied'));
    },
    [t, messageApi],
  );

  const open = useCallback((url: string) => {
    if (url) window.open(url, '_blank');
  }, []);

  const tabs = useMemo(() => {
    const items: NonNullable<TabsProps['items']> = [];
    if (subUrl || subJsonUrl || subClashUrl) {
      items.push({
        key: 'subscription',
        icon: <LinkOutlined />,
        label: t('subscription.tabLinks'),
        children: (
          <SubLinksTab
            subUrl={subUrl}
            subJsonUrl={subJsonUrl}
            subClashUrl={subClashUrl}
            onCopy={copy}
          />
        ),
      });
    }
    if (subUrl) {
      items.push({
        key: 'apps',
        icon: <AppstoreOutlined />,
        label: t('subscription.tabApps'),
        children: <SubAppsTab apps={apps} initialPlatform={initialPlatform} onOpen={open} />,
      });
    }
    if (telemtData.enabled) {
      items.push({
        key: 'telemt',
        icon: <ApiOutlined />,
        label: 'Telegram Proxy',
        children: (
          <Space direction="vertical" size="middle" style={{ width: '100%' }}>
            {telemtError && (
              <Alert
                type="warning"
                showIcon
                title="Telegram Proxy data is temporarily unavailable"
                description="The subscription is still available. Check the Telemt and Web Proxy settings, then reload this page."
              />
            )}

            <Card size="small" title="Personal Telegram Proxy">
              {telemtData.personal?.link ? (
                <Space direction="vertical" size="small" style={{ width: '100%' }}>
                  <Typography.Text type="secondary">
                    Individual proxy profile for this subscription · {telemtData.personal.host}:
                    {telemtData.personal.port}
                  </Typography.Text>
                  <Typography.Paragraph
                    copyable={{ text: telemtData.personal.link }}
                    style={{ marginBottom: 0 }}
                  >
                    <Typography.Link href={telemtData.personal.link}>
                      {telemtData.personal.link}
                    </Typography.Link>
                  </Typography.Paragraph>
                  <Button
                    type="primary"
                    icon={<ApiOutlined />}
                    onClick={() => open(telemtData.personal!.link)}
                  >
                    Open in Telegram
                  </Button>
                </Space>
              ) : (
                <Typography.Text type="secondary">
                  {telemtLoading
                    ? 'Loading personal Telemt profile…'
                    : telemtError
                      ? 'Unable to load the personal Telemt profile.'
                      : 'Personal Telemt proxy is currently unavailable for this subscription.'}
                </Typography.Text>
              )}
            </Card>

            {telemtData.webProxyEnabled && (
              <Card size="small" title="Web Proxy">
                {telemtData.webProxy ? (
                  <Space direction="vertical" size="small" style={{ width: '100%' }}>
                    <Typography.Text type="secondary">
                      Shared Web Proxy for all subscription users
                    </Typography.Text>
                    <Typography.Paragraph
                      copyable={{ text: telemtData.webProxy }}
                      style={{ marginBottom: 0 }}
                    >
                      <Typography.Link href={telemtData.webProxy}>
                        {telemtData.webProxy}
                      </Typography.Link>
                    </Typography.Paragraph>
                    <Button
                      type="primary"
                      icon={<ApiOutlined />}
                      onClick={() => open(telemtData.webProxy!)}
                    >
                      Open in Telegram
                    </Button>
                  </Space>
                ) : (
                  <Typography.Text type="secondary">
                    {telemtLoading
                      ? 'Loading shared Web Proxy…'
                      : telemtError
                        ? 'Unable to load the shared Web Proxy.'
                        : 'Shared Web Proxy is enabled but currently unavailable.'}
                  </Typography.Text>
                )}
              </Card>
            )}
          </Space>
        ),
      });
    }
    if (links.length > 0) {
      items.push({
        key: 'configs',
        icon: <UnorderedListOutlined />,
        label: (
          <>
            {t('subscription.tabConfigs')}
            <span className="sub-tab-count">{links.length}</span>
          </>
        ),
        children: <SubConfigsTab links={links} onCopy={copy} />,
      });
    }
    return items;
  }, [t, copy, open, telemtData, telemtLoading, telemtError]);

  const direction = RTL_LANGUAGES.has(lang) ? 'rtl' : 'ltr';
  const pageClass = ['subscription-page', isDark && 'is-dark', isUltra && 'is-ultra']
    .filter(Boolean)
    .join(' ');

  const themeConfig = useMemo(() => {
    const accent = isDark ? ACCENT.dark : ACCENT.light;
    const primary = {
      colorPrimary: accent.primary,
      colorPrimaryHover: accent.hover,
      colorPrimaryActive: accent.active,
    };
    return {
      ...antdThemeConfig,
      token: {
        ...antdThemeConfig.token,
        ...primary,
        colorLink: accent.primary,
        colorInfo: accent.primary,
      },
      components: {
        ...antdThemeConfig.components,
        Button: { ...antdThemeConfig.components?.Button, ...primary },
        Progress: { ...antdThemeConfig.components?.Progress, remainingColor: accent.rail },
      },
    };
  }, [antdThemeConfig, isDark]);

  return (
    <ConfigProvider theme={themeConfig} direction={direction}>
      {messageContextHolder}
      <Layout className={pageClass} dir={direction}>
        <div className="sub-aurora" aria-hidden="true">
          <span className="sub-aurora-grid" />
        </div>
        <Layout.Content className="sub-content">
          <Card className="sub-card">
            <SubHeader
              title={subTitle}
              sId={sId}
              email={clientEmail}
              lang={lang}
              onLangChange={onLangChange}
            />
            {announce && <Alert type="info" showIcon title={announce} className="sub-announce" />}
            <SubHero {...heroData} lang={lang} />
            {tabs.length > 0 && <Tabs className="sub-tabs" tabBarGutter={24} items={tabs} />}
            {(updateHours > 0 || subSupportUrl) && (
              <footer className="sub-footer">
                {updateHours > 0 && (
                  <span>
                    <ClockCircleOutlined />
                    {t('subscription.updateInterval', { hours: updateHours })}
                  </span>
                )}
                {subSupportUrl && (
                  <a href={subSupportUrl} target="_blank" rel="noopener noreferrer">
                    <CustomerServiceOutlined />
                    {t('subscription.support')}
                  </a>
                )}
              </footer>
            )}
          </Card>
        </Layout.Content>
      </Layout>
    </ConfigProvider>
  );
}
