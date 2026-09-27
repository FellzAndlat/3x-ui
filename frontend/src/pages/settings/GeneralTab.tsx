import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useLocation, useNavigate } from 'react-router';
import { Alert, Button, Input, InputNumber, Modal, Radio, Select, Space, Switch, Tabs, Tag, message } from 'antd';
import {
  ApartmentOutlined,
  BellOutlined,
  ClockCircleOutlined,
  GlobalOutlined,
  SafetyCertificateOutlined,
  SettingOutlined,
  ReloadOutlined,
  SwapOutlined,
} from '@ant-design/icons';
import type { AllSetting } from '@/models/setting';
import { isOutboundProtocol } from '@/schemas/primitives';
import { HttpUtil, LanguageManager } from '@/utils';
import { onNumber } from '@/utils/onNumber';
import { DefaultSettingTag, SettingListItem } from '@/components/ui';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import { catTabLabel } from './catTabLabel';
import { sanitizePath } from './uriPath';
import SecretInput from './SecretInput';

interface ApiMsg<T = unknown> {
  success?: boolean;
  obj?: T;
}

interface GeneralTabProps {
  allSetting: AllSetting;
  updateSetting: (patch: Partial<AllSetting>) => void;
  saveSettings?: () => Promise<{ success?: boolean; msg?: string }>;
  onOpenSwap?: () => void;
  onOpenSystemUpdate?: () => void;
}

export default function GeneralTab({
  allSetting,
  updateSetting,
  saveSettings,
  onOpenSwap,
  onOpenSystemUpdate,
}: GeneralTabProps) {
  const { t } = useTranslation();
  const { isMobile } = useMediaQuery();
  const location = useLocation();
  const navigate = useNavigate();
  const hashToTab: Record<string, string> = {
    '#general': '1',
    '#core': 'core',
    '#notifications': '2',
    '#certs': '3',
    '#external': '4',
    '#datetime': '5',
    '#ldap': '6',
  };
  const activeTab = hashToTab[location.hash] ?? '1';

  const onTabChange = (key: string) => {
    const tabToHash: Record<string, string> = {
      '1': 'general',
      core: 'core',
      '2': 'notifications',
      '3': 'certs',
      '4': 'external',
      '5': 'datetime',
      '6': 'ldap',
    };
    const hash = tabToHash[key];
    if (hash) navigate('/settings#' + hash);
  };

  const [lang, setLang] = useState<string>(() => LanguageManager.getLanguage());
  const [inboundOptions, setInboundOptions] = useState<{ label: string; value: string }[]>([]);
  const [outboundTagList, setOutboundTagList] = useState<string[]>([]);
  const [balancerTagList, setBalancerTagList] = useState<string[]>([]);
  const [singBoxInstalled, setSingBoxInstalled] = useState<boolean | null>(null);
  const [singBoxInstalling, setSingBoxInstalling] = useState(false);
  const [runningCore, setRunningCore] = useState<'xray' | 'sing-box' | 'none'>('none');
  const [hiddifyOpen, setHiddifyOpen] = useState(false);
  const [hiddifyFile, setHiddifyFile] = useState<File | null>(null);
  const [hiddifyPreview, setHiddifyPreview] = useState<{ users: number; warnings: string[] } | null>(null);
  const [hiddifyResult, setHiddifyResult] = useState<{ created: number; skipped: { email: string; reason: string }[] } | null>(null);
  const [hiddifyBusy, setHiddifyBusy] = useState(false);

  const hiddifyRequest = async (action: 'preview' | 'import', file: File) => {
    const form = new FormData();
    form.append('backup', file);
    return HttpUtil.post<{ users?: number; warnings?: string[]; created?: number; skipped?: unknown[] }>(
      `/panel/api/server/hiddify/${action}`, form, { silentSuccess: true },
    );
  };

  const previewHiddify = async (file: File) => {
    setHiddifyFile(file);
    setHiddifyPreview(null);
    setHiddifyResult(null);
    setHiddifyBusy(true);
    try {
      const result = await hiddifyRequest('preview', file);
      if (result.success && typeof result.obj?.users === 'number') {
        setHiddifyPreview({ users: result.obj.users, warnings: result.obj.warnings ?? [] });
      }
    } finally { setHiddifyBusy(false); }
  };

  const importHiddify = async () => {
    if (!hiddifyFile || !hiddifyPreview) return;
    setHiddifyBusy(true);
    try {
      const result = await hiddifyRequest('import', hiddifyFile);
      if (result.success) {
        const skipped = (result.obj?.skipped ?? []) as { email: string; reason: string }[];
        const created = result.obj?.created ?? 0;
        setHiddifyResult({ created, skipped });
        if (skipped.length === 0) {
          message.success(`Импортировано пользователей: ${created}.`);
          setHiddifyOpen(false);
          setHiddifyFile(null);
          setHiddifyPreview(null);
        }
      }
    } finally { setHiddifyBusy(false); }
  };

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setInterval> | undefined;

    const refreshCoreStatus = async () => {
      try {
        const [singMsg, serverMsg] = await Promise.all([
          HttpUtil.get('/panel/api/setting/singbox/status') as Promise<
            ApiMsg<{ installed: boolean; running: boolean }>
          >,
          HttpUtil.get('/panel/api/server/status') as Promise<
            ApiMsg<{ xray?: { state?: string } }>
          >,
        ]);
        if (cancelled) return false;
        setSingBoxInstalled(Boolean(singMsg?.success && singMsg.obj?.installed));
        const singRunning = Boolean(singMsg?.success && singMsg.obj?.running);
        const xrayRunning = Boolean(serverMsg?.success && serverMsg.obj?.xray?.state === 'running');
        const actualCore = singRunning ? 'sing-box' : xrayRunning ? 'xray' : 'none';
        setRunningCore(actualCore);
        return actualCore === allSetting.coreType;
      } catch {
        if (!cancelled) setRunningCore('none');
        return false;
      }
    };

    void refreshCoreStatus().then((matched) => {
      if (cancelled || matched) return;
      // Core restart happens on the server after settings are saved. Poll only
      // while the selected core and the actually running core differ, so the
      // status card updates immediately after the switch completes.
      timer = setInterval(() => {
        void refreshCoreStatus().then((done) => {
          if (done && timer) {
            clearInterval(timer);
            timer = undefined;
          }
        });
      }, 1000);
    });

    const onCoreSettingsSaved = () => {
      void refreshCoreStatus();
    };
    window.addEventListener('xui-core-settings-saved', onCoreSettingsSaved);

    return () => {
      cancelled = true;
      if (timer) clearInterval(timer);
      window.removeEventListener('xui-core-settings-saved', onCoreSettingsSaved);
    };
  }, [allSetting.coreType]);

  const installSingBox = async () => {
    setSingBoxInstalling(true);
    try {
      const msg = (await HttpUtil.post('/panel/api/setting/singbox/install')) as ApiMsg;
      if (!msg?.success) return;
      setSingBoxInstalled(true);

      // The radio choice is kept as a draft until the normal settings save.
      // Once the binary is installed, finish that pending core switch here so
      // the selected engine becomes the actually running engine immediately.
      if (allSetting.coreType === 'sing-box' && saveSettings) {
        await saveSettings();
      }
    } finally {
      setSingBoxInstalling(false);
    }
  };

  const uninstallSingBox = async () => {
    const msg = (await HttpUtil.post('/panel/api/setting/singbox/uninstall')) as ApiMsg;
    if (!msg?.success) return;
    setSingBoxInstalled(false);
    setRunningCore('none');
  };

  useEffect(() => {
    let cancelled = false;
    (async () => {
      // /options is the slim picker-shaped endpoint — it skips the heavy
      // per-client settings and clientStats payloads that /list ships.
      const msg = (await HttpUtil.get('/panel/api/inbounds/options')) as ApiMsg<
        {
          tag: string;
          protocol: string;
          port: number;
        }[]
      >;
      if (cancelled) return;
      if (msg?.success && Array.isArray(msg.obj)) {
        setInboundOptions(
          msg.obj.map((ib) => ({
            label: `${ib.tag} (${ib.protocol}@${ib.port})`,
            value: ib.tag,
          })),
        );
      } else {
        setInboundOptions([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      // Candidates for the panel egress picker: template outbounds plus
      // subscription-derived outbounds, and routing balancers. The panel egress
      // is injected as a routing rule, so a balancer tag is a valid target
      // (it load-balances the panel's own traffic). The geodata picker, by
      // contrast, dials a forced tag and can only use a concrete outbound.
      const msg = (await HttpUtil.post('/panel/api/xray/', undefined, {
        silent: true,
      })) as ApiMsg<string>;
      if (cancelled || !msg?.success || typeof msg.obj !== 'string') return;
      try {
        const payload = JSON.parse(msg.obj) as Record<string, unknown>;
        const template = (payload.xraySetting || {}) as Record<string, unknown>;
        const tags = new Set<string>();
        const outbounds = Array.isArray(template.outbounds) ? template.outbounds : [];
        for (const o of outbounds) {
          if (!o || typeof o !== 'object') continue;
          const rec = o as Record<string, unknown>;
          if (isOutboundProtocol(rec, 'blackhole')) continue; // never a useful egress
          const tag = rec.tag;
          if (typeof tag === 'string' && tag) tags.add(tag);
        }
        const subTags = Array.isArray(payload.subscriptionOutboundTags)
          ? payload.subscriptionOutboundTags
          : [];
        for (const tag of subTags) {
          if (typeof tag === 'string' && tag) tags.add(tag);
        }
        const balancerTags: string[] = [];
        const routing = (template.routing || {}) as Record<string, unknown>;
        const balancers = Array.isArray(routing.balancers) ? routing.balancers : [];
        for (const b of balancers) {
          if (!b || typeof b !== 'object') continue;
          const tag = (b as Record<string, unknown>).tag;
          if (typeof tag === 'string' && tag && !tags.has(tag)) balancerTags.push(tag);
        }
        setOutboundTagList([...tags]);
        setBalancerTagList(balancerTags);
      } catch {
        setOutboundTagList([]);
        setBalancerTagList([]);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Outbound tags and balancer tags share one picker. When balancers exist they
  // get their own labeled group so it's clear the selection routes through a
  // balancer rather than a single outbound.
  const outboundOptions = useMemo<
    (
      | { label: string; value: string }
      | { label: string; options: { label: string; value: string }[] }
    )[]
  >(() => {
    const outOpts = outboundTagList.map((tag) => ({ label: tag, value: tag }));
    if (balancerTagList.length === 0) return outOpts;
    return [
      { label: t('pages.xray.Outbounds'), options: outOpts },
      {
        label: t('pages.xray.Balancers'),
        options: balancerTagList.map((tag) => ({ label: tag, value: tag })),
      },
    ];
  }, [outboundTagList, balancerTagList, t]);

  const ldapInboundTagList = useMemo(() => {
    const csv = allSetting.ldapInboundTags || '';
    return csv.length
      ? csv
          .split(',')
          .map((s) => s.trim())
          .filter(Boolean)
      : [];
  }, [allSetting.ldapInboundTags]);

  function setLdapInboundTagList(list: string[]) {
    updateSetting({ ldapInboundTags: Array.isArray(list) ? list.join(',') : '' });
  }

  function onLangChange(value: string) {
    setLang(value);
    LanguageManager.setLanguage(value);
  }

  const langOptions = useMemo(
    () =>
      LanguageManager.supportedLanguages.map(
        (l: { value: string; name: string; icon: string }) => ({
          value: l.value,
          label: (
            <>
              <span role="img" aria-label={l.name}>
                {l.icon}
              </span>
              &nbsp;&nbsp;<span>{l.name}</span>
            </>
          ),
        }),
      ),
    [],
  );

  return (
    <>
    <Tabs
      activeKey={activeTab}
      onChange={onTabChange}
      items={[
        {
          key: '1',
          label: catTabLabel(<SettingOutlined />, t('pages.settings.panelSettings'), isMobile),
          children: (
            <div>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.panelListeningIP')}
                description={t('pages.settings.panelListeningIPDesc')}
              >
                <Input
                  value={allSetting.webListen}
                  onChange={(e) => updateSetting({ webListen: e.target.value })}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.panelListeningDomain')}
                description={t('pages.settings.panelListeningDomainDesc')}
              >
                <Input
                  value={allSetting.webDomain}
                  onChange={(e) => updateSetting({ webDomain: e.target.value })}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.panelPort')}
                badge={<DefaultSettingTag settingKey="webPort" value={allSetting.webPort} />}
                description={t('pages.settings.panelPortDesc')}
              >
                <InputNumber
                  value={allSetting.webPort}
                  min={1}
                  max={65535}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ webPort: v }))}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.panelUrlPath')}
                description={t('pages.settings.panelUrlPathDesc')}
              >
                <Input
                  value={allSetting.webBasePath}
                  onChange={(e) => updateSetting({ webBasePath: sanitizePath(e.target.value) })}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.sessionMaxAge')}
                badge={
                  <DefaultSettingTag settingKey="sessionMaxAge" value={allSetting.sessionMaxAge} />
                }
                description={t('pages.settings.sessionMaxAgeDesc')}
              >
                <InputNumber
                  value={allSetting.sessionMaxAge}
                  min={60}
                  max={525600}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ sessionMaxAge: v }))}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.trustedProxyCidrs')}
                description={t('pages.settings.trustedProxyCidrsDesc')}
              >
                <Input
                  value={allSetting.trustedProxyCIDRs}
                  placeholder="127.0.0.1/32,::1/128"
                  onChange={(e) => updateSetting({ trustedProxyCIDRs: e.target.value })}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.realityScanCandidates')}
                description={t('pages.settings.realityScanCandidatesDesc')}
                badge={
                  <DefaultSettingTag
                    settingKey="realityScanCandidates"
                    value={allSetting.realityScanCandidates}
                  />
                }
              >
                <Input.TextArea
                  rows={3}
                  value={allSetting.realityScanCandidates}
                  placeholder="www.cloudflare.com:443,www.microsoft.com:443"
                  onChange={(e) => updateSetting({ realityScanCandidates: e.target.value })}
                />
              </SettingListItem>

              <SettingListItem
                paddings="small"
                title={t('pages.settings.ipLimitAllowlist')}
                description={t('pages.settings.ipLimitAllowlistDesc')}
              >
                <Input
                  value={allSetting.ipLimitAllowlist}
                  placeholder="203.0.113.10,198.51.100.0/24"
                  onChange={(e) => updateSetting({ ipLimitAllowlist: e.target.value })}
                />
              </SettingListItem>

              {allSetting.coreType !== 'sing-box' && (
                <SettingListItem
                  paddings="small"
                  title={t('pages.settings.panelOutbound')}
                  description={t('pages.settings.panelOutboundDesc')}
                >
                  <Select
                    style={{ width: '100%' }}
                    allowClear
                    showSearch
                    value={allSetting.panelOutbound || undefined}
                    placeholder={t('pages.settings.panelOutboundPh')}
                    options={outboundOptions}
                    onChange={(v) =>
                      updateSetting({ panelOutbound: (v as string | undefined) || '' })
                    }
                  />
                </SettingListItem>
              )}

              <SettingListItem
                paddings="small"
                title={t('pages.settings.pageSize')}
                badge={<DefaultSettingTag settingKey="pageSize" value={allSetting.pageSize} />}
                description={t('pages.settings.pageSizeDesc')}
              >
                <InputNumber
                  value={allSetting.pageSize}
                  min={0}
                  max={1000}
                  step={5}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ pageSize: v }))}
                />
              </SettingListItem>

              {allSetting.coreType !== 'sing-box' && (
                <SettingListItem
                  paddings="small"
                  title={t('pages.settings.restartXrayOnClientDisable')}
                  description={t('pages.settings.restartXrayOnClientDisableDesc')}
                >
                  <Switch
                    checked={allSetting.restartXrayOnClientDisable}
                    onChange={(v) => updateSetting({ restartXrayOnClientDisable: v })}
                  />
                </SettingListItem>
              )}

              <SettingListItem paddings="small" title={t('pages.settings.language')}>
                <Select
                  value={lang}
                  onChange={onLangChange}
                  style={{ width: '100%' }}
                  options={langOptions}
                />
              </SettingListItem>
            </div>
          ),
        },
        {
          key: 'core',
          label: catTabLabel(<SettingOutlined />, t('pages.settings.coreType'), isMobile),
          children: (
            <div>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.coreType')}
                description={t('pages.settings.coreTypeDesc')}
              >
                <div style={{ display: 'flex', flexDirection: 'column', gap: 8, width: '100%' }}>
                  <Space direction="vertical" size={12} style={{ width: '100%' }}>
                    <Radio.Group
                      value={allSetting.coreType || 'xray'}
                      onChange={(e) =>
                        updateSetting({ coreType: e.target.value as 'xray' | 'sing-box' })
                      }
                      optionType="button"
                      buttonStyle="solid"
                      size="large"
                      style={{ display: 'flex', width: '100%' }}
                    >
                      <Radio.Button value="xray" style={{ flex: 1, textAlign: 'center' }}>
                        <strong>Xray</strong>
                      </Radio.Button>
                      <Radio.Button value="sing-box" style={{ flex: 1, textAlign: 'center' }}>
                        <strong>sing-box</strong>
                      </Radio.Button>
                    </Radio.Group>

                    <div
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        gap: 12,
                        padding: '10px 12px',
                        borderRadius: 10,
                        background: 'var(--ant-color-fill-quaternary)',
                      }}
                    >
                      <div style={{ minWidth: 0 }}>
                        <div style={{ fontWeight: 600 }}>
                          {allSetting.coreType === 'sing-box' ? 'sing-box' : 'Xray'}
                        </div>
                        <div style={{ fontSize: 12, opacity: 0.65 }}>
                          {allSetting.coreType === 'sing-box'
                            ? singBoxInstalled
                              ? t('pages.settings.singBoxInstalled')
                              : t('pages.settings.installSingBox')
                            : t('pages.settings.coreTypeDesc')}
                        </div>
                        <div style={{ fontSize: 12, marginTop: 4 }}>
                          Работает сейчас:{' '}
                          <strong>
                            {runningCore === 'sing-box'
                              ? 'sing-box'
                              : runningCore === 'xray'
                                ? 'Xray'
                                : 'нет'}
                          </strong>
                        </div>
                      </div>
                      {allSetting.coreType === 'sing-box' && (
                        <Tag color={singBoxInstalled ? 'success' : 'warning'}>
                          {singBoxInstalled ? 'Installed' : 'Not installed'}
                        </Tag>
                      )}
                    </div>

                    {allSetting.coreType === 'sing-box' && !singBoxInstalled && (
                      <Button
                        type="primary"
                        block
                        loading={singBoxInstalling}
                        onClick={installSingBox}
                      >
                        {t('pages.settings.installSingBox')}
                      </Button>
                    )}

                    {allSetting.coreType === 'sing-box' && singBoxInstalled && (
                      <Button
                        block
                        danger
                        onClick={uninstallSingBox}
                        disabled={runningCore === 'sing-box'}
                      >
                        Удалить sing-box
                      </Button>
                    )}
                    {allSetting.coreType === 'sing-box' &&
                      singBoxInstalled &&
                      runningCore === 'sing-box' && (
                        <div style={{ fontSize: 12, opacity: 0.65 }}>
                          Сначала переключите ядро на Xray и сохраните настройки.
                        </div>
                      )}
                    <Space wrap style={{ width: '100%' }}>
                      {allSetting.coreType === 'sing-box' && (
                        <Button onClick={() => setHiddifyOpen(true)}>
                          Импорт пользователей Hiddify
                        </Button>
                      )}
                      <Button icon={<SwapOutlined />} onClick={() => onOpenSwap?.()}>
                        {t('pages.settings.swap.openFromCore')}
                      </Button>
                      <Button icon={<ReloadOutlined />} onClick={() => onOpenSystemUpdate?.()}>
                        {t('pages.settings.swap.systemUpdates')}
                      </Button>
                    </Space>
                  </Space>
                </div>
              </SettingListItem>
            </div>
          ),
        },
        {
          key: '2',
          label: catTabLabel(<BellOutlined />, t('pages.settings.notifications'), isMobile),
          children: (
            <>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.expireTimeDiff')}
                badge={<DefaultSettingTag settingKey="expireDiff" value={allSetting.expireDiff} />}
                description={t('pages.settings.expireTimeDiffDesc')}
              >
                <InputNumber
                  value={allSetting.expireDiff}
                  min={0}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ expireDiff: v }))}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.trafficDiff')}
                badge={
                  <DefaultSettingTag settingKey="trafficDiff" value={allSetting.trafficDiff} />
                }
                description={t('pages.settings.trafficDiffDesc')}
              >
                <InputNumber
                  value={allSetting.trafficDiff}
                  min={0}
                  max={100}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ trafficDiff: v }))}
                />
              </SettingListItem>
            </>
          ),
        },
        {
          key: '3',
          label: catTabLabel(<SafetyCertificateOutlined />, t('pages.settings.certs'), isMobile),
          children: (
            <>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.publicKeyPath')}
                description={t('pages.settings.publicKeyPathDesc')}
              >
                <Input
                  value={allSetting.webCertFile}
                  onChange={(e) => updateSetting({ webCertFile: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.privateKeyPath')}
                description={t('pages.settings.privateKeyPathDesc')}
              >
                <Input
                  value={allSetting.webKeyFile}
                  onChange={(e) => updateSetting({ webKeyFile: e.target.value })}
                />
              </SettingListItem>
            </>
          ),
        },
        {
          key: '4',
          label: catTabLabel(<GlobalOutlined />, t('pages.settings.externalTraffic'), isMobile),
          children: (
            <>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.externalTrafficInformEnable')}
                description={t('pages.settings.externalTrafficInformEnableDesc')}
              >
                <Switch
                  checked={allSetting.externalTrafficInformEnable}
                  onChange={(v) => updateSetting({ externalTrafficInformEnable: v })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.externalTrafficInformURI')}
                description={t('pages.settings.externalTrafficInformURIDesc')}
              >
                <Input
                  value={allSetting.externalTrafficInformURI}
                  placeholder="(http|https)://domain[:port]/path/"
                  onChange={(e) => updateSetting({ externalTrafficInformURI: e.target.value })}
                />
              </SettingListItem>
            </>
          ),
        },
        {
          key: '5',
          label: catTabLabel(<ClockCircleOutlined />, t('pages.settings.dateAndTime'), isMobile),
          children: (
            <>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.timeZone')}
                description={t('pages.settings.timeZoneDesc')}
              >
                <Input
                  value={allSetting.timeLocation}
                  onChange={(e) => updateSetting({ timeLocation: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.datepicker')}
                description={t('pages.settings.datepickerDescription')}
              >
                <Select
                  value={allSetting.datepicker || 'gregorian'}
                  onChange={(v) => updateSetting({ datepicker: v as 'gregorian' | 'jalalian' })}
                  style={{ width: '100%' }}
                  options={[
                    { value: 'gregorian', label: t('pages.settings.calendarGregorian') },
                    { value: 'jalalian', label: t('pages.settings.calendarJalalian') },
                  ]}
                />
              </SettingListItem>
            </>
          ),
        },
        {
          key: '6',
          label: catTabLabel(<ApartmentOutlined />, 'LDAP', isMobile),
          children: (
            <>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.enable')}>
                <Switch
                  checked={allSetting.ldapEnable}
                  onChange={(v) => updateSetting({ ldapEnable: v })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.host')}>
                <Input
                  value={allSetting.ldapHost}
                  onChange={(e) => updateSetting({ ldapHost: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.port')}
                badge={<DefaultSettingTag settingKey="ldapPort" value={allSetting.ldapPort} />}
              >
                <InputNumber
                  value={allSetting.ldapPort}
                  min={1}
                  max={65535}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ ldapPort: v }))}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.useTls')}>
                <Switch
                  checked={allSetting.ldapUseTLS}
                  onChange={(v) => updateSetting({ ldapUseTLS: v })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.skipTlsVerify')}
                description={t('pages.settings.ldap.skipTlsVerifyDesc')}
              >
                <Switch
                  checked={allSetting.ldapInsecureSkipVerify}
                  disabled={!allSetting.ldapUseTLS}
                  onChange={(v) => updateSetting({ ldapInsecureSkipVerify: v })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.bindDn')}>
                <Input
                  value={allSetting.ldapBindDN}
                  onChange={(e) => updateSetting({ ldapBindDN: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('password')}
                description={
                  allSetting.hasLdapPassword && !allSetting.clearLdapPassword
                    ? t('pages.settings.ldap.passwordConfigured')
                    : t('pages.settings.ldap.passwordUnconfigured')
                }
              >
                <SecretInput
                  value={allSetting.ldapPassword}
                  configured={allSetting.hasLdapPassword}
                  clearArmed={allSetting.clearLdapPassword}
                  placeholder={t('pages.settings.ldap.passwordPlaceholder')}
                  onChange={(v) => updateSetting({ ldapPassword: v })}
                  onClearArmedChange={(armed) => updateSetting({ clearLdapPassword: armed })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.baseDn')}>
                <Input
                  value={allSetting.ldapBaseDN}
                  onChange={(e) => updateSetting({ ldapBaseDN: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.userFilter')}>
                <Input
                  value={allSetting.ldapUserFilter}
                  onChange={(e) => updateSetting({ ldapUserFilter: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.userAttr')}>
                <Input
                  value={allSetting.ldapUserAttr}
                  onChange={(e) => updateSetting({ ldapUserAttr: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.vlessField')}>
                <Input
                  value={allSetting.ldapVlessField}
                  onChange={(e) => updateSetting({ ldapVlessField: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.flagField')}
                description={t('pages.settings.ldap.flagFieldDesc')}
              >
                <Input
                  value={allSetting.ldapFlagField}
                  onChange={(e) => updateSetting({ ldapFlagField: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.truthyValues')}
                description={t('pages.settings.ldap.truthyValuesDesc')}
              >
                <Input
                  value={allSetting.ldapTruthyValues}
                  onChange={(e) => updateSetting({ ldapTruthyValues: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.invertFlag')}
                description={t('pages.settings.ldap.invertFlagDesc')}
              >
                <Switch
                  checked={allSetting.ldapInvertFlag}
                  onChange={(v) => updateSetting({ ldapInvertFlag: v })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.syncSchedule')}
                description={t('pages.settings.ldap.syncScheduleDesc')}
              >
                <Input
                  value={allSetting.ldapSyncCron}
                  onChange={(e) => updateSetting({ ldapSyncCron: e.target.value })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.inboundTags')}
                description={t('pages.settings.ldap.inboundTagsDesc')}
              >
                <>
                  <Select
                    mode="multiple"
                    value={ldapInboundTagList}
                    onChange={setLdapInboundTagList}
                    style={{ width: '100%' }}
                    options={inboundOptions}
                  />
                  {inboundOptions.length === 0 && (
                    <div className="ldap-no-inbounds">{t('pages.settings.ldap.noInbounds')}</div>
                  )}
                </>
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.autoCreate')}>
                <Switch
                  checked={allSetting.ldapAutoCreate}
                  onChange={(v) => updateSetting({ ldapAutoCreate: v })}
                />
              </SettingListItem>
              <SettingListItem paddings="small" title={t('pages.settings.ldap.autoDelete')}>
                <Switch
                  checked={allSetting.ldapAutoDelete}
                  onChange={(v) => updateSetting({ ldapAutoDelete: v })}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.defaultTotalGb')}
                badge={
                  <DefaultSettingTag
                    settingKey="ldapDefaultTotalGB"
                    value={allSetting.ldapDefaultTotalGB}
                  />
                }
              >
                <InputNumber
                  value={allSetting.ldapDefaultTotalGB}
                  min={0}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ ldapDefaultTotalGB: v }))}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.defaultExpiryDays')}
                badge={
                  <DefaultSettingTag
                    settingKey="ldapDefaultExpiryDays"
                    value={allSetting.ldapDefaultExpiryDays}
                  />
                }
              >
                <InputNumber
                  value={allSetting.ldapDefaultExpiryDays}
                  min={0}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ ldapDefaultExpiryDays: v }))}
                />
              </SettingListItem>
              <SettingListItem
                paddings="small"
                title={t('pages.settings.ldap.defaultIpLimit')}
                badge={
                  <DefaultSettingTag
                    settingKey="ldapDefaultLimitIP"
                    value={allSetting.ldapDefaultLimitIP}
                  />
                }
              >
                <InputNumber
                  value={allSetting.ldapDefaultLimitIP}
                  min={0}
                  style={{ width: '100%' }}
                  onChange={onNumber((v) => updateSetting({ ldapDefaultLimitIP: v }))}
                />
              </SettingListItem>
            </>
          ),
        },
      ]}
    />
    <Modal
      open={hiddifyOpen}
      title="Импорт пользователей Hiddify"
      okText="Импортировать пользователей"
      okButtonProps={{ disabled: !hiddifyPreview, loading: hiddifyBusy }}
      onOk={() => void importHiddify()}
      onCancel={() => { setHiddifyOpen(false); setHiddifyFile(null); setHiddifyPreview(null); setHiddifyResult(null); }}
    >
      <p>Выберите JSON резервной копии Hiddify. Подключения и настройки сервера не импортируются.</p>
      <input
        type="file"
        accept=".json,application/json"
        aria-label="Резервная копия Hiddify JSON"
        onChange={(event) => {
          const file = event.target.files?.[0];
          if (file) void previewHiddify(file);
        }}
      />
      {hiddifyBusy && <p>Обработка резервной копии…</p>}
      {hiddifyPreview && (
        <Alert
          style={{ marginTop: 16 }}
          type="info"
          showIcon
          message={`Пользователей в копии: ${hiddifyPreview.users}`}
          description={
            <ul style={{ paddingLeft: 20, marginBottom: 0 }}>
              {hiddifyPreview.warnings.map((warning) => <li key={warning}>{warning}</li>)}
            </ul>
          }
        />
      )}
      {hiddifyResult && hiddifyResult.skipped.length > 0 && (
        <Alert
          style={{ marginTop: 12 }}
          type="warning"
          showIcon
          message={`Добавлено: ${hiddifyResult.created}. Пропущено: ${hiddifyResult.skipped.length}.`}
          description={hiddifyResult.skipped.map((item) => <div key={item.email}>{item.email}: {item.reason}</div>)}
        />
      )}
    </Modal>
    </>
  );
}
