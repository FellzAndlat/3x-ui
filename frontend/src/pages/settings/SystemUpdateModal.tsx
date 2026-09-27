import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Modal,
  Pagination,
  Popconfirm,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from 'antd';
import {
  CloudDownloadOutlined,
  DownloadOutlined,
  PoweroffOutlined,
  ReloadOutlined,
} from '@ant-design/icons';

import { HttpUtil } from '@/utils';
import { useMediaQuery } from '@/hooks/useMediaQuery';
import './SystemUpdateModal.css';

type SystemUpdatePackage = {
  name: string;
  installedVersion: string;
  availableVersion: string;
  installed: boolean;
  required: boolean;
  kernel: boolean;
  updateAvailable: boolean;
};

type DependencyKey = 'naiveproxy' | 'hysteria2' | 'sudoku' | 'pingtunnel' | 'trusttunnel';

type DependencyStatus = {
  key: DependencyKey;
  label: string;
  installed: boolean;
  installedVersion: string;
  availableVersion: string;
  updateAvailable: boolean;
  prerelease?: boolean;
  source?: 'xray' | 'sing-box';
};

type ExternalVPNBinaryStatus = {
  installed?: boolean;
  version?: string;
  latestVersion?: string;
  updateAvailable?: boolean;
  error?: string;
};

type ExternalVPNStatus = {
  pingtunnel?: ExternalVPNBinaryStatus;
  trusttunnel?: ExternalVPNBinaryStatus;
};

type NaiveProxyStatus = {
  installed?: boolean;
  version?: string;
  latestVersion?: string;
  updateAvailable?: boolean;
};

type CoreType = 'xray' | 'sing-box';

type SystemUpdateStatus = {
  distribution: string;
  version: string;
  packageManager: string;
  supported: boolean;
  runningAsRoot: boolean;
  packages: SystemUpdatePackage[];
  kernel: {
    runningVersion: string;
    updateAvailable: boolean;
    availableVersion: string;
    packageNames: string[];
    rebootRequired: boolean;
  };
  updatesAvailable: boolean;
  missingPackages: boolean;
  canUpdate: boolean;
  allPackages: boolean;
  notes: string[];
};

type SystemUpdateResult = {
  updated: boolean;
  rebootRequired: boolean;
  output: string;
  error: string;
};

type ApiMsg<T = unknown> = { success?: boolean; msg?: string; obj?: T };

const normalizeVersion = (value: string) => value.trim().replace(/^v/i, '');

const versionsDiffer = (installed: string, available: string) =>
  normalizeVersion(installed) !== normalizeVersion(available);

const isDependencyInstallable = (dependency: DependencyStatus) =>
  dependency.key === 'sudoku' ||
  dependency.key === 'pingtunnel' ||
  dependency.key === 'trusttunnel' ||
  (dependency.key === 'naiveproxy' && dependency.source === 'xray');

const dependencyNeedsAction = (dependency: DependencyStatus) => {
  if (!dependency.availableVersion) return false;
  if (isDependencyInstallable(dependency)) {
    return !dependency.installed || dependency.updateAvailable;
  }
  return dependency.installed && dependency.updateAvailable;
};

function waitForUpdateRecovery(delayMs: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, delayMs);
  });
}

function isTransientFetchFailure(message: string): boolean {
  return /failed to fetch|networkerror|network error|load failed/i.test(message);
}

function normalizeSystemUpdate(value: unknown): SystemUpdateStatus {
  const raw =
    value && typeof value === 'object' && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {};
  const kernel =
    raw.kernel && typeof raw.kernel === 'object' ? (raw.kernel as Record<string, unknown>) : {};
  const packages = Array.isArray(raw.packages) ? raw.packages : [];
  return {
    distribution: typeof raw.distribution === 'string' ? raw.distribution : '',
    version: typeof raw.version === 'string' ? raw.version : '',
    packageManager: typeof raw.packageManager === 'string' ? raw.packageManager : '',
    supported: raw.supported !== false,
    runningAsRoot: raw.runningAsRoot === true,
    packages: packages
      .filter((item) => item && typeof item === 'object')
      .map((item) => {
        const row = item as Record<string, unknown>;
        return {
          name: typeof row.name === 'string' ? row.name : '',
          installedVersion: typeof row.installedVersion === 'string' ? row.installedVersion : '',
          availableVersion: typeof row.availableVersion === 'string' ? row.availableVersion : '',
          installed: row.installed === true,
          required: row.required === true,
          kernel: row.kernel === true,
          updateAvailable: row.updateAvailable === true,
        };
      }),
    kernel: {
      runningVersion: typeof kernel.runningVersion === 'string' ? kernel.runningVersion : '',
      updateAvailable: kernel.updateAvailable === true,
      availableVersion: typeof kernel.availableVersion === 'string' ? kernel.availableVersion : '',
      packageNames: Array.isArray(kernel.packageNames)
        ? kernel.packageNames.filter((item): item is string => typeof item === 'string')
        : [],
      rebootRequired: kernel.rebootRequired === true,
    },
    updatesAvailable: raw.updatesAvailable === true,
    missingPackages: raw.missingPackages === true,
    canUpdate: raw.canUpdate === true,
    allPackages: raw.allPackages === true,
    notes: Array.isArray(raw.notes)
      ? raw.notes.filter((item): item is string => typeof item === 'string')
      : [],
  };
}

function normalizeSystemUpdateResult(value: unknown): SystemUpdateResult {
  const raw =
    value && typeof value === 'object' && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {};
  return {
    updated: raw.updated === true,
    rebootRequired: raw.rebootRequired === true,
    output: typeof raw.output === 'string' ? raw.output : '',
    error: typeof raw.error === 'string' ? raw.error : '',
  };
}

export default function SystemUpdateModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { isMobile } = useMediaQuery();
  const [messageApi, contextHolder] = message.useMessage();
  const [systemUpdateBusy, setSystemUpdateBusy] = useState(false);
  const [systemUpdate, setSystemUpdate] = useState<SystemUpdateStatus | null>(null);
  const [systemUpdateResult, setSystemUpdateResult] = useState<SystemUpdateResult | null>(null);
  const [includeAllPackages, setIncludeAllPackages] = useState(false);
  const [packagePage, setPackagePage] = useState(1);
  const [dependencies, setDependencies] = useState<DependencyStatus[]>([]);
  const [dependencyBusy, setDependencyBusy] = useState<DependencyKey | null>(null);

  const loadDependencyUpdates = useCallback(async () => {
    const safeGet = async <T,>(url: string): Promise<ApiMsg<T>> => {
      try {
        return (await HttpUtil.get<T>(url, undefined, { silent: true })) as ApiMsg<T>;
      } catch {
        return { success: false };
      }
    };
    const safePost = async <T,>(url: string): Promise<ApiMsg<T>> => {
      try {
        return (await HttpUtil.post(url, undefined, { silent: true })) as ApiMsg<T>;
      } catch {
        return { success: false };
      }
    };

    const settings = await safePost<{ coreType?: string }>('/panel/api/setting/all');
    const coreType: CoreType | null =
      settings.success &&
      (settings.obj?.coreType === 'xray' || settings.obj?.coreType === 'sing-box')
        ? settings.obj.coreType
        : null;

    const [
      serverStatus,
      xrayVersions,
      singBoxStatus,
      singBoxVersions,
      naiveProxyStatus,
      sudokuStatus,
      externalVPNStatus,
    ] = await Promise.all([
      safeGet<{ xray?: { version?: string } }>('/panel/api/server/status'),
      safeGet<string[]>('/panel/api/server/getXrayVersion'),
      safeGet<{ installed?: boolean; version?: string }>('/panel/api/setting/singbox/status'),
      safeGet<Array<{ version?: string; prerelease?: boolean } | string>>(
        '/panel/api/setting/singbox/versions',
      ),
      coreType === 'xray'
        ? safeGet<NaiveProxyStatus>('/panel/api/naiveproxy/status')
        : Promise.resolve({ success: false } as ApiMsg<NaiveProxyStatus>),
      safeGet<{
        installed?: boolean;
        version?: string;
        latestVersion?: string;
        updateAvailable?: boolean;
      }>('/panel/api/setting/sudoku/status'),
      safeGet<ExternalVPNStatus>('/panel/api/server/externalvpn/status'),
    ]);

    const xrayCurrent = serverStatus.success ? serverStatus.obj?.xray?.version || '' : '';
    const xrayLatest =
      xrayVersions.success && Array.isArray(xrayVersions.obj) ? xrayVersions.obj[0] || '' : '';

    const singBoxCurrent = singBoxStatus.success ? singBoxStatus.obj?.version || '' : '';
    const singBoxInstalled =
      singBoxStatus.success === true && singBoxStatus.obj?.installed === true;
    const singBoxVersionList =
      singBoxVersions.success && Array.isArray(singBoxVersions.obj)
        ? singBoxVersions.obj
            .map((item) =>
              typeof item === 'string'
                ? { version: item, prerelease: false }
                : {
                    version: typeof item.version === 'string' ? item.version : '',
                    prerelease: item.prerelease === true,
                  },
            )
            .filter((item) => Boolean(item.version))
        : [];
    const stableSingBoxVersion =
      singBoxVersionList.find((item) => !item.prerelease)?.version ||
      singBoxVersionList[0]?.version ||
      '';

    const standaloneNaiveInstalled =
      naiveProxyStatus.success === true && naiveProxyStatus.obj?.installed === true;
    const standaloneNaiveCurrent = naiveProxyStatus.success
      ? naiveProxyStatus.obj?.version || ''
      : '';
    const standaloneNaiveLatest = naiveProxyStatus.success
      ? naiveProxyStatus.obj?.latestVersion || ''
      : '';
    const naiveInstalled =
      coreType === 'sing-box'
        ? singBoxInstalled
        : coreType === 'xray'
          ? standaloneNaiveInstalled
          : false;
    const naiveCurrent =
      coreType === 'sing-box' ? singBoxCurrent : coreType === 'xray' ? standaloneNaiveCurrent : '';
    const naiveLatest =
      coreType === 'sing-box'
        ? stableSingBoxVersion
        : coreType === 'xray'
          ? standaloneNaiveLatest
          : '';

    const sudokuCurrent = sudokuStatus.success ? sudokuStatus.obj?.version || '' : '';
    const sudokuLatest = sudokuStatus.success ? sudokuStatus.obj?.latestVersion || '' : '';
    const pingtunnel = externalVPNStatus.success ? externalVPNStatus.obj?.pingtunnel : undefined;
    const trusttunnel = externalVPNStatus.success ? externalVPNStatus.obj?.trusttunnel : undefined;

    setDependencies([
      {
        key: 'naiveproxy',
        label: 'NaiveProxy',
        source: coreType || undefined,
        installed: naiveInstalled,
        installedVersion: naiveCurrent,
        availableVersion: naiveLatest,
        updateAvailable: Boolean(
          naiveInstalled &&
          naiveCurrent &&
          naiveLatest &&
          versionsDiffer(naiveCurrent, naiveLatest),
        ),
      },
      {
        key: 'hysteria2',
        label: 'Hysteria2',
        source: coreType || undefined,
        installed: Boolean(coreType && (coreType === 'sing-box' ? singBoxCurrent : xrayCurrent)),
        installedVersion:
          coreType === 'sing-box' ? singBoxCurrent : coreType === 'xray' ? xrayCurrent : '',
        availableVersion:
          coreType === 'sing-box' ? stableSingBoxVersion : coreType === 'xray' ? xrayLatest : '',
        updateAvailable: Boolean(
          coreType &&
          (coreType === 'sing-box'
            ? singBoxCurrent &&
              stableSingBoxVersion &&
              versionsDiffer(singBoxCurrent, stableSingBoxVersion)
            : xrayCurrent && xrayLatest && versionsDiffer(xrayCurrent, xrayLatest)),
        ),
      },
      {
        key: 'sudoku',
        label: 'Sudoku',
        installed: sudokuStatus.success === true && sudokuStatus.obj?.installed === true,
        installedVersion: sudokuCurrent,
        availableVersion: sudokuLatest,
        updateAvailable:
          sudokuStatus.success && sudokuStatus.obj
            ? sudokuStatus.obj.updateAvailable === true
            : Boolean(sudokuCurrent && sudokuLatest && versionsDiffer(sudokuCurrent, sudokuLatest)),
      },
      {
        key: 'pingtunnel',
        label: 'Pingtunnel',
        installed: pingtunnel?.installed === true,
        installedVersion: pingtunnel?.version || '',
        availableVersion: pingtunnel?.latestVersion || '',
        updateAvailable:
          pingtunnel?.updateAvailable === true ||
          Boolean(
            pingtunnel?.installed &&
            pingtunnel.version &&
            pingtunnel.latestVersion &&
            versionsDiffer(pingtunnel.version, pingtunnel.latestVersion),
          ),
      },
      {
        key: 'trusttunnel',
        label: 'TrustTunnel',
        installed: trusttunnel?.installed === true,
        installedVersion: trusttunnel?.version || '',
        availableVersion: trusttunnel?.latestVersion || '',
        updateAvailable:
          trusttunnel?.updateAvailable === true ||
          Boolean(
            trusttunnel?.installed &&
            trusttunnel.version &&
            trusttunnel.latestVersion &&
            versionsDiffer(trusttunnel.version, trusttunnel.latestVersion),
          ),
      },
    ]);
  }, []);

  const checkSystemUpdates = useCallback(async () => {
    setSystemUpdateBusy(true);
    try {
      const [msg] = await Promise.all([
        HttpUtil.post(
          `/panel/api/setting/system/update/check?allPackages=${includeAllPackages}`,
        ) as Promise<ApiMsg<unknown>>,
        loadDependencyUpdates(),
      ]);
      if (!msg?.success) throw new Error(msg?.msg || 'Failed to check system updates');
      setSystemUpdate(normalizeSystemUpdate(msg.obj));
      setSystemUpdateResult(null);
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSystemUpdateBusy(false);
    }
  }, [includeAllPackages, loadDependencyUpdates, messageApi]);

  const requestDependencyUpdate = useCallback(async (dependency: DependencyStatus) => {
    switch (dependency.key) {
      case 'naiveproxy':
        if (dependency.source === 'xray') {
          return (await HttpUtil.post('/panel/api/naiveproxy/update')) as ApiMsg<unknown>;
        }
        return (await HttpUtil.post(
          `/panel/api/setting/singbox/install/${encodeURIComponent(dependency.availableVersion)}`,
        )) as ApiMsg<unknown>;
      case 'hysteria2':
        if (dependency.source === 'sing-box') {
          return (await HttpUtil.post(
            `/panel/api/setting/singbox/install/${encodeURIComponent(dependency.availableVersion)}`,
          )) as ApiMsg<unknown>;
        }
        return (await HttpUtil.post(
          `/panel/api/server/installXray/${encodeURIComponent(dependency.availableVersion)}`,
        )) as ApiMsg<unknown>;
      case 'sudoku':
        return (await HttpUtil.post('/panel/api/setting/sudoku/update')) as ApiMsg<unknown>;
      case 'pingtunnel':
      case 'trusttunnel':
        return (await HttpUtil.post(
          `/panel/api/server/externalvpn/update/${dependency.key}`,
        )) as ApiMsg<unknown>;
    }
  }, []);

  const updateDependency = async (dependency: DependencyStatus) => {
    if (!dependencyNeedsAction(dependency)) return;

    setDependencyBusy(dependency.key);
    try {
      const response = await requestDependencyUpdate(dependency);
      if (!response?.success) {
        throw new Error(response?.msg || `Не удалось обновить ${dependency.label}`);
      }

      messageApi.success(`${dependency.label}: ${t('pages.settings.swap.updateDone')}`);
      await loadDependencyUpdates();
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : String(error));
    } finally {
      setDependencyBusy(null);
    }
  };

  const recoverSystemUpdateStatus = useCallback(async (): Promise<SystemUpdateStatus | null> => {
    const delays = [0, 1000, 2000, 4000, 6000];

    for (const delay of delays) {
      if (delay > 0) {
        await waitForUpdateRecovery(delay);
      }

      const status = await HttpUtil.get(
        `/panel/api/setting/system/update/status?allPackages=${includeAllPackages}`,
        undefined,
        {
          silent: true,
          timeout: 15000,
        },
      );
      if (status.success) {
        const normalized = normalizeSystemUpdate(status.obj);
        setSystemUpdate(normalized);
        return normalized;
      }
    }

    return null;
  }, [includeAllPackages]);

  const applyAllUpdates = async () => {
    setSystemUpdateBusy(true);
    try {
      const systemMsg = (await HttpUtil.post(
        `/panel/api/setting/system/update/apply?allPackages=${includeAllPackages}`,
      )) as ApiMsg<unknown>;
      let systemUpdateRecovered = Boolean(systemMsg?.success);

      if (!systemMsg?.success) {
        const result = normalizeSystemUpdateResult(systemMsg.obj);
        setSystemUpdateResult(result);

        const errorMessage =
          systemMsg?.msg || result.error || t('pages.settings.swap.updateFailed');
        if (isTransientFetchFailure(errorMessage)) {
          const recoveredStatus = await recoverSystemUpdateStatus();
          systemUpdateRecovered = recoveredStatus !== null;

          if (systemUpdateRecovered) {
            messageApi.info('Соединение с панелью восстановлено; состояние обновления проверено.');
          }
        }

        if (!systemUpdateRecovered) {
          throw new Error(errorMessage);
        }
      }

      if (systemMsg?.success) {
        setSystemUpdateResult(normalizeSystemUpdateResult(systemMsg.obj));
      }

      const pending = dependencies.filter(dependencyNeedsAction);

      const failed: string[] = [];
      const updatedTargets = new Set<string>();
      for (const dependency of pending) {
        const target =
          dependency.key === 'naiveproxy'
            ? dependency.source === 'xray'
              ? 'naiveproxy'
              : 'sing-box'
            : dependency.key === 'hysteria2'
              ? dependency.source || 'xray'
              : dependency.key;
        if (updatedTargets.has(target)) {
          continue;
        }
        try {
          updatedTargets.add(target);
          setDependencyBusy(dependency.key);
          const response = await requestDependencyUpdate(dependency);
          if (!response?.success) {
            failed.push(dependency.label);
          }
        } catch {
          failed.push(dependency.label);
        } finally {
          setDependencyBusy(null);
        }
      }

      await recoverSystemUpdateStatus();
      await loadDependencyUpdates();

      if (failed.length) {
        messageApi.warning(
          t('pages.settings.swap.partialUpdate', { components: failed.join(', ') }),
        );
      } else {
        messageApi.success(t('pages.settings.swap.updateDone'));
      }
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSystemUpdateBusy(false);
      setDependencyBusy(null);
    }
  };

  const rebootSystem = async () => {
    setSystemUpdateBusy(true);
    try {
      const response = (await HttpUtil.post('/panel/api/setting/system/update/reboot')) as ApiMsg<{
        rebooting?: boolean;
      }>;
      if (!response?.success) {
        throw new Error(response?.msg || t('pages.settings.swap.rebootFailed'));
      }

      messageApi.info(t('pages.settings.swap.rebootStarted'));
      onClose();
      window.setTimeout(() => window.location.reload(), 8000);
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSystemUpdateBusy(false);
    }
  };

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => {
      void checkSystemUpdates();
    }, 0);
    return () => window.clearTimeout(timer);
  }, [checkSystemUpdates, open]);

  const systemUpdateRows = useMemo(
    () =>
      (systemUpdate?.packages ?? [])
        .filter((item) => systemUpdate?.allPackages || item.required || item.kernel)
        .map((item) => ({
          ...item,
          key: item.kernel ? 'kernel:' + item.name : 'package:' + item.name,
        }))
        .sort((a, b) => {
          if (a.updateAvailable !== b.updateAvailable) return a.updateAvailable ? -1 : 1;
          if (a.kernel !== b.kernel) return a.kernel ? -1 : 1;
          return a.name.localeCompare(b.name);
        }),
    [systemUpdate],
  );

  const rebootRequired =
    Boolean(systemUpdate?.kernel.rebootRequired) || Boolean(systemUpdateResult?.rebootRequired);
  const allPackagesMode = systemUpdate?.allPackages ?? includeAllPackages;
  const isArch = systemUpdate?.packageManager === 'pacman';
  const componentUpdatesAvailable = dependencies.some(dependencyNeedsAction);
  const unstableComponentUpdateAvailable = dependencies.some(
    (dependency) => dependency.prerelease && dependency.updateAvailable,
  );

  const renderPackageStatus = (row: SystemUpdatePackage) => (
    <Space size={4} wrap>
      <Tag color={row.kernel ? 'geekblue' : 'blue'}>
        {row.kernel
          ? t('pages.settings.swap.kernelPackage')
          : row.required
            ? t('pages.settings.swap.dependency')
            : t('pages.settings.swap.otherPackage')}
      </Tag>
      {row.updateAvailable && (
        <Tag color="warning">{t('pages.settings.swap.updatesAvailable')}</Tag>
      )}
      {!row.installed && <Tag color="error">{t('pages.settings.swap.notInstalled')}</Tag>}
    </Space>
  );

  return (
    <>
      {contextHolder}
      <Modal
        rootClassName="system-update-modal"
        open={open}
        title={t('pages.settings.swap.systemUpdatesTitle')}
        width={isMobile ? 'calc(100vw - 16px)' : 980}
        centered={!isMobile}
        onCancel={() => {
          if (!systemUpdateBusy && !dependencyBusy) onClose();
        }}
        footer={
          <div className="system-update-actions">
            <Button
              block={isMobile}
              icon={<ReloadOutlined />}
              onClick={() => void checkSystemUpdates()}
              loading={systemUpdateBusy}
              disabled={dependencyBusy !== null}
            >
              {t('pages.settings.swap.checkUpdates')}
            </Button>
            {rebootRequired && (
              <Button
                danger
                block={isMobile}
                icon={<PoweroffOutlined />}
                onClick={() => void rebootSystem()}
                loading={systemUpdateBusy}
                disabled={!systemUpdate?.runningAsRoot || dependencyBusy !== null}
              >
                {t('pages.settings.swap.rebootSystem')}
              </Button>
            )}
            <Popconfirm
              title={t(
                allPackagesMode
                  ? 'pages.settings.swap.updateAllConfirm'
                  : 'pages.settings.swap.updateConfirm',
              )}
              description={
                unstableComponentUpdateAvailable
                  ? t('pages.settings.swap.unstableUpdateConfirm')
                  : undefined
              }
              onConfirm={() => void applyAllUpdates()}
              disabled={
                !systemUpdate?.supported ||
                !systemUpdate?.runningAsRoot ||
                systemUpdateBusy ||
                dependencyBusy !== null ||
                (!systemUpdate.canUpdate && !componentUpdatesAvailable)
              }
            >
              <Button
                type="primary"
                block={isMobile}
                icon={<CloudDownloadOutlined />}
                loading={systemUpdateBusy}
                disabled={
                  !systemUpdate?.supported ||
                  !systemUpdate?.runningAsRoot ||
                  dependencyBusy !== null ||
                  (!systemUpdate.canUpdate && !componentUpdatesAvailable)
                }
              >
                {t(
                  allPackagesMode
                    ? 'pages.settings.swap.updateAllNow'
                    : 'pages.settings.swap.updateNow',
                )}
              </Button>
            </Popconfirm>
          </div>
        }
      >
        {!systemUpdate ? (
          <Alert type="info" showIcon title={t('pages.settings.swap.checkUpdates')} />
        ) : (
          <div className="system-update-content">
            <Space wrap>
              <Switch
                checked={allPackagesMode}
                disabled={isArch || systemUpdateBusy || dependencyBusy !== null}
                onChange={(checked) => {
                  setIncludeAllPackages(checked);
                  setPackagePage(1);
                  setSystemUpdate(null);
                  setSystemUpdateResult(null);
                }}
              />
              <Typography.Text>{t('pages.settings.swap.allPackagesOption')}</Typography.Text>
            </Space>
            {isArch && (
              <Alert type="info" showIcon title={t('pages.settings.swap.archFullUpgradeNote')} />
            )}
            <Descriptions
              size="small"
              bordered
              column={{ xs: 1, sm: 2, lg: 3 }}
              className="system-update-overview"
            >
              <Descriptions.Item label={t('pages.settings.swap.distro')}>
                {systemUpdate.distribution} {systemUpdate.version}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.settings.swap.packageManager')}>
                {systemUpdate.packageManager}
              </Descriptions.Item>
              <Descriptions.Item label={t('pages.settings.swap.currentKernel')}>
                {systemUpdate.kernel.runningVersion || '—'}
              </Descriptions.Item>
            </Descriptions>

            {!systemUpdate.supported && (
              <Alert
                type="error"
                showIcon
                title={t('pages.settings.swap.unsupportedSystemUpdate')}
              />
            )}
            {!systemUpdate.runningAsRoot && (
              <Alert type="error" showIcon title={t('pages.settings.swap.notRoot')} />
            )}
            {!isArch && (
              <Alert type="info" showIcon title={t('pages.settings.swap.systemUpdateNote')} />
            )}
            {systemUpdate.missingPackages && (
              <Alert type="warning" showIcon title={t('pages.settings.swap.missingPackages')} />
            )}
            {systemUpdate.updatesAvailable || componentUpdatesAvailable ? (
              <Alert
                type="warning"
                showIcon
                title={t('pages.settings.swap.updatesAvailable')}
                description={
                  systemUpdate.kernel.updateAvailable
                    ? t('pages.settings.swap.rebootAfterKernel')
                    : undefined
                }
              />
            ) : (
              !systemUpdate.missingPackages && (
                <Alert type="success" showIcon title={t('pages.settings.swap.noUpdates')} />
              )
            )}

            {rebootRequired && (
              <Alert
                type="warning"
                showIcon
                message={t('pages.settings.swap.rebootRequired')}
                description={t('pages.settings.swap.rebootAfterKernel')}
              />
            )}

            <Card
              size="small"
              title={t('pages.settings.swap.componentUpdatesTitle')}
              className="system-update-section"
            >
              <div className="system-component-grid">
                {dependencies.map((dependency) => (
                  <Card
                    key={dependency.key}
                    size="small"
                    className="system-component-card"
                    title={dependency.label}
                    extra={
                      dependency.updateAvailable ? (
                        <Tag color="warning">{t('pages.settings.swap.updatesAvailable')}</Tag>
                      ) : dependency.installed ? (
                        <Tag color="success">{t('pages.settings.swap.upToDate')}</Tag>
                      ) : (
                        <Tag>{t('pages.settings.swap.notInstalled')}</Tag>
                      )
                    }
                  >
                    {dependency.key === 'hysteria2' && (
                      <Typography.Text
                        type="secondary"
                        style={{ display: 'block', marginBottom: 10 }}
                      >
                        {dependency.source === 'sing-box'
                          ? 'Hysteria2 обновляется вместе с sing-box, поскольку используется реализация Hysteria2 из sing-box.'
                          : 'Hysteria2 обновляется вместе с Xray-core, поскольку используется реализация Hysteria2 из Xray.'}
                      </Typography.Text>
                    )}
                    {dependency.key === 'naiveproxy' && (
                      <Typography.Text
                        type="secondary"
                        style={{ display: 'block', marginBottom: 10 }}
                      >
                        {dependency.source === 'xray'
                          ? 'При Xray NaiveProxy используется как отдельный бинарник и обновляется напрямую из официальных релизов klzgrad/forwardproxy.'
                          : 'При sing-box NaiveProxy используется из встроенной реализации sing-box и обновляется вместе с sing-box.'}
                      </Typography.Text>
                    )}
                    {dependency.key === 'pingtunnel' && (
                      <Typography.Text
                        type="secondary"
                        style={{ display: 'block', marginBottom: 10 }}
                      >
                        Pingtunnel обновляется отдельным официальным Linux-бинарником из GitHub
                        Releases.
                      </Typography.Text>
                    )}
                    {dependency.key === 'trusttunnel' && (
                      <Typography.Text
                        type="secondary"
                        style={{ display: 'block', marginBottom: 10 }}
                      >
                        TrustTunnel обновляется отдельным официальным endpoint-бинарником из GitHub
                        Releases.
                      </Typography.Text>
                    )}
                    <div className="system-component-meta">
                      <div>
                        <Typography.Text type="secondary">
                          {t('pages.settings.swap.installedVersion')}
                        </Typography.Text>
                        <Typography.Text strong>
                          {dependency.installed ? dependency.installedVersion || '—' : '—'}
                        </Typography.Text>
                      </div>
                      <div>
                        <Typography.Text type="secondary">
                          {t('pages.settings.swap.availableVersion')}
                        </Typography.Text>
                        <Typography.Text strong>
                          {dependency.availableVersion || '—'}
                        </Typography.Text>
                      </div>
                    </div>
                    <Button
                      size="small"
                      icon={<DownloadOutlined />}
                      loading={dependencyBusy === dependency.key}
                      disabled={
                        (!dependency.updateAvailable &&
                          !(isDependencyInstallable(dependency) && !dependency.installed)) ||
                        !dependency.availableVersion ||
                        dependencyBusy !== null ||
                        systemUpdateBusy
                      }
                      onClick={() => void updateDependency(dependency)}
                    >
                      {!dependency.installed && isDependencyInstallable(dependency)
                        ? 'Установить'
                        : dependency.updateAvailable
                          ? t('pages.settings.swap.updateComponent')
                          : t('pages.settings.swap.upToDate')}
                    </Button>
                  </Card>
                ))}
              </div>
            </Card>

            <Card
              size="small"
              title={t('pages.settings.swap.systemPackagesTitle')}
              className="system-update-section"
            >
              {isMobile ? (
                <div className="system-package-list">
                  {systemUpdateRows.slice((packagePage - 1) * 20, packagePage * 20).map((row) => (
                    <Card key={row.key} size="small" className="system-package-card">
                      <Typography.Text strong>{row.name}</Typography.Text>
                      <div className="system-package-meta">
                        <span>
                          {t('pages.settings.swap.installedVersion')}:&nbsp;
                          {row.installed
                            ? row.installedVersion || '—'
                            : t('pages.settings.swap.notInstalled')}
                        </span>
                        <span>
                          {t('pages.settings.swap.availableVersion')}:&nbsp;
                          {row.availableVersion || '—'}
                        </span>
                      </div>
                      {renderPackageStatus(row)}
                    </Card>
                  ))}
                  {systemUpdateRows.length > 20 && (
                    <Pagination
                      simple
                      current={packagePage}
                      total={systemUpdateRows.length}
                      pageSize={20}
                      onChange={setPackagePage}
                    />
                  )}
                </div>
              ) : (
                <Table
                  size="small"
                  pagination={systemUpdateRows.length > 20 ? { pageSize: 20 } : false}
                  rowKey="key"
                  dataSource={systemUpdateRows}
                  columns={[
                    {
                      title: t('pages.settings.swap.device'),
                      dataIndex: 'name',
                      key: 'name',
                    },
                    {
                      title: t('pages.settings.swap.installedVersion'),
                      key: 'installedVersion',
                      render: (_: unknown, row: SystemUpdatePackage) =>
                        row.installed
                          ? row.installedVersion || '—'
                          : t('pages.settings.swap.notInstalled'),
                    },
                    {
                      title: t('pages.settings.swap.availableVersion'),
                      key: 'availableVersion',
                      render: (_: unknown, row: SystemUpdatePackage) => row.availableVersion || '—',
                    },
                    {
                      title: t('pages.settings.swap.status'),
                      key: 'status',
                      render: (_: unknown, row: SystemUpdatePackage) => renderPackageStatus(row),
                    },
                  ]}
                />
              )}
            </Card>

            {systemUpdate.kernel.updateAvailable && (
              <Alert
                type="warning"
                showIcon
                title={
                  t('pages.settings.swap.availableKernel') +
                  ': ' +
                  (systemUpdate.kernel.availableVersion ||
                    systemUpdate.kernel.packageNames.join(', '))
                }
              />
            )}

            {systemUpdate.notes.map((note) => (
              <Alert key={note} type="info" showIcon title={note} />
            ))}

            {systemUpdateResult?.output && (
              <Card size="small" title={t('pages.settings.swap.updateOutput')}>
                <Typography.Text code>
                  <pre className="system-update-output">{systemUpdateResult.output}</pre>
                </Typography.Text>
              </Card>
            )}
          </div>
        )}
      </Modal>
    </>
  );
}
