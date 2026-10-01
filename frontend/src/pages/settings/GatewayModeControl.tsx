import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Modal, Popconfirm, Space, Tag, Tooltip, Typography, message } from 'antd';
import { ApartmentOutlined, ReloadOutlined } from '@ant-design/icons';

import { HttpUtil } from '@/utils';

type ApiMsg<T = unknown> = {
  success?: boolean;
  msg?: string;
  obj?: T;
};

type GatewayStatus = {
  enabled: boolean;
  configured: boolean;
  recoveryBackup: boolean;
  canEnable: boolean;
  coreType: string;
  xrayRunning: boolean;
  port: number;
};

export default function GatewayModeControl() {
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState<GatewayStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [messageApi, contextHolder] = message.useMessage();

  const loadStatus = useCallback(
    async (quiet = false): Promise<GatewayStatus | null> => {
      try {
        const response = (await HttpUtil.get('/panel/api/gateway/status', undefined, {
          silent: true,
        })) as ApiMsg<GatewayStatus>;
        if (!response?.success || !response.obj) {
          if (!quiet) {
            messageApi.error(response?.msg || 'Не удалось получить состояние Gateway Mode');
          }
          return null;
        }
        return response.obj;
      } catch (error) {
        if (!quiet) {
          messageApi.error(
            error instanceof Error ? error.message : 'Не удалось получить состояние Gateway Mode',
          );
        }
        return null;
      }
    },
    [messageApi],
  );

  const refresh = useCallback(
    async (quiet = false) => {
      const nextStatus = await loadStatus(quiet);
      if (nextStatus) setStatus(nextStatus);
      setLoading(false);
    },
    [loadStatus],
  );

  useEffect(() => {
    let active = true;

    void loadStatus()
      .then((nextStatus) => {
        if (active && nextStatus) setStatus(nextStatus);
      })
      .finally(() => {
        if (active) setLoading(false);
      });

    return () => {
      active = false;
    };
  }, [loadStatus]);

  useEffect(() => {
    const onSettingsSaved = () => void refresh(true);
    window.addEventListener('xui-core-settings-saved', onSettingsSaved);
    return () => window.removeEventListener('xui-core-settings-saved', onSettingsSaved);
  }, [refresh]);

  const runAction = async (action: 'enable' | 'disable') => {
    setBusy(true);
    try {
      const response = (await HttpUtil.post(`/panel/api/gateway/${action}`, undefined, {
        silentSuccess: true,
      })) as ApiMsg<GatewayStatus>;

      if (response?.obj) setStatus(response.obj);
      if (!response?.success) {
        messageApi.error(response?.msg || 'Не удалось изменить Gateway Mode');
        return;
      }

      messageApi.success(action === 'enable' ? 'Gateway Mode включён' : 'Gateway Mode выключен');
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : 'Не удалось изменить Gateway Mode');
    } finally {
      await refresh(true);
      setBusy(false);
    }
  };

  const enabled = status?.enabled === true;
  const configured = status?.configured === true;
  const recoveryOnly = enabled && !configured && status?.recoveryBackup === true;
  const singBoxSelected = status?.coreType === 'sing-box';
  const canEnable = status?.canEnable === true;

  return (
    <>
      {contextHolder}
      <Tooltip title="Управление прозрачным шлюзом Xray">
        <Button
          icon={<ApartmentOutlined />}
          loading={loading}
          onClick={() => {
            setOpen(true);
            void refresh(true);
          }}
        >
          Gateway: {recoveryOnly ? 'восстановление' : enabled ? 'вкл.' : 'выкл.'}
        </Button>
      </Tooltip>

      <Modal
        open={open}
        title="Gateway Mode"
        footer={null}
        destroyOnClose={false}
        onCancel={() => setOpen(false)}
      >
        <Space direction="vertical" size={14} style={{ width: '100%' }}>
          <Alert
            type={recoveryOnly ? 'warning' : enabled ? 'success' : 'info'}
            showIcon
            title={
              recoveryOnly
                ? 'Требуется очистка состояния Gateway Mode'
                : enabled
                  ? 'Gateway Mode включён'
                  : 'Gateway Mode выключен'
            }
            description={
              recoveryOnly
                ? 'Найдена резервная копия Gateway Mode, но его объекты отсутствуют в текущем Xray-конфиге. Нажмите «Выключить», чтобы безопасно удалить устаревший recovery-marker.'
                : enabled
                  ? 'В шаблоне Xray активен отдельный TPROXY-вход. Дальнейшая маршрутизация выполняется обычными правилами Xray.'
                  : 'Gateway-вход сейчас не активен в конфигурации Xray.'
            }
          />

          <Space wrap>
            <Typography.Text strong>Ядро:</Typography.Text>
            <Tag color={singBoxSelected ? 'gold' : 'blue'}>
              {singBoxSelected ? 'sing-box' : 'Xray'}
            </Tag>
            <Tag color={status?.xrayRunning ? 'success' : 'default'}>
              Xray {status?.xrayRunning ? 'работает' : 'остановлен'}
            </Tag>
            <Tag>TPROXY: {status?.port ?? 52345}</Tag>
          </Space>

          {singBoxSelected && !enabled && (
            <Alert
              type="warning"
              showIcon
              title="Для включения выберите Xray"
              description="Gateway Mode меняет конфигурацию Xray и недоступен для включения, пока выбрано ядро sing-box."
            />
          )}

          {singBoxSelected && enabled && !recoveryOnly && (
            <Alert
              type="warning"
              showIcon
              title="Gateway Mode активен в Xray"
              description="Сейчас выбрано ядро sing-box. Gateway Mode можно безопасно выключить: будет удалён только его собственный TPROXY-вход и старые служебные артефакты Gateway Mode, если они остались от предыдущей реализации."
            />
          )}

          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            При включении панель сохраняет резервную копию текущего шаблона Xray и добавляет
            отдельный TPROXY-вход. Outbound и routing не подменяются: перехваченный трафик проходит
            через ваши обычные правила маршрутизации Xray.
          </Typography.Paragraph>

          <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
            Linux должен перенаправлять TCP/UDP через TPROXY на порт {status?.port ?? 52345} и иметь
            включённый IPv4 forwarding. Клиентские устройства должны использовать этот сервер как
            шлюз.
          </Typography.Paragraph>

          <Space wrap>
            {enabled ? (
              <Popconfirm
                title="Выключить Gateway Mode?"
                description={
                  recoveryOnly
                    ? 'Будет удалён только устаревший recovery-marker. Текущий Xray-конфиг не будет откатан.'
                    : 'Будет удалён Gateway TPROXY-вход и совместимые служебные артефакты старой реализации. Остальные настройки Xray сохранятся.'
                }
                okText="Выключить"
                cancelText="Отмена"
                onConfirm={() => void runAction('disable')}
              >
                <Button danger loading={busy}>
                  Выключить Gateway Mode
                </Button>
              </Popconfirm>
            ) : (
              <Popconfirm
                title="Включить Gateway Mode?"
                description="Панель сохранит резервную копию и добавит TPROXY-вход в текущий шаблон Xray, не меняя ваши outbound и routing."
                okText="Включить"
                cancelText="Отмена"
                onConfirm={() => void runAction('enable')}
                disabled={!canEnable || loading}
              >
                <Button type="primary" loading={busy} disabled={!canEnable || loading}>
                  Включить Gateway Mode
                </Button>
              </Popconfirm>
            )}
            <Button icon={<ReloadOutlined />} disabled={busy} onClick={() => void refresh()}>
              Обновить
            </Button>
          </Space>
        </Space>
      </Modal>
    </>
  );
}
