import { useCallback, useEffect, useState } from 'react';
import { Alert, Button, Popconfirm, Space, Tag, Typography, message } from 'antd';
import { ReloadOutlined } from '@ant-design/icons';

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
  gatewayCoreType?: string;
  coreMismatch?: boolean;
  conflict?: boolean;
  port: number;
};

function coreLabel(core?: string) {
  if (core === 'sing-box') return 'sing-box';
  if (core === 'xray') return 'Xray';
  if (core === 'multiple') return 'Xray + sing-box';
  return 'не определено';
}

export default function GatewayModeControl() {
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
            messageApi.error(response?.msg || 'Не удалось получить состояние режима шлюза');
          }
          return null;
        }
        return response.obj;
      } catch (error) {
        if (!quiet) {
          messageApi.error(
            error instanceof Error ? error.message : 'Не удалось получить состояние режима шлюза',
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
        messageApi.error(response?.msg || 'Не удалось изменить режим шлюза');
        return;
      }

      messageApi.success(
        action === 'enable' ? 'Режим шлюза включён' : 'Режим шлюза выключен',
      );
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : 'Не удалось изменить режим шлюза');
    } finally {
      await refresh(true);
      setBusy(false);
    }
  };

  const enabled = status?.enabled === true;
  const configured = status?.configured === true;
  const coreMismatch = status?.coreMismatch === true;
  const conflict = status?.conflict === true;
  const recoveryOnly =
    enabled && !configured && !coreMismatch && status?.recoveryBackup === true;
  const canEnable = status?.canEnable === true;
  const selectedCore = coreLabel(status?.coreType);
  const ownerCore = coreLabel(status?.gatewayCoreType);

  return (
    <>
      {contextHolder}
      <Space direction="vertical" size={14} style={{ width: '100%' }}>
        <div>
          <Typography.Title level={4} style={{ margin: 0 }}>
            Режим шлюза
          </Typography.Title>
          <Typography.Text type="secondary">
            Прозрачная маршрутизация TCP/UDP через TPROXY для выбранного ядра.
          </Typography.Text>
        </div>

        <Alert
          type={conflict ? 'error' : recoveryOnly || coreMismatch ? 'warning' : enabled ? 'success' : 'info'}
          showIcon
          title={
            conflict
              ? 'Обнаружен конфликт режима шлюза'
              : recoveryOnly
                ? 'Требуется очистка состояния режима шлюза'
                : coreMismatch
                  ? `Режим шлюза активен на другом ядре: ${ownerCore}`
                  : enabled
                    ? 'Режим шлюза включён'
                    : 'Режим шлюза выключен'
          }
          description={
            conflict
              ? 'Служебная конфигурация режима шлюза обнаружена одновременно в Xray и sing-box. Выключение безопасно очистит собственные объекты режима шлюза в обоих ядрах.'
              : recoveryOnly
                ? 'Найдена резервная копия режима шлюза, но его служебная конфигурация отсутствует. Нажмите «Выключить», чтобы безопасно удалить устаревшее состояние восстановления.'
                : coreMismatch
                  ? `Сейчас выбрано ядро ${selectedCore}. Режим шлюза можно перенести на него без ручной очистки предыдущего ядра.`
                  : enabled
                    ? `Режим шлюза настроен для ${ownerCore === 'не определено' ? selectedCore : ownerCore}. Дальнейшая маршрутизация выполняется обычными правилами ядра.`
                    : `Режим шлюза сейчас не активен. При включении он будет настроен для ${selectedCore}.`
          }
        />

        <Space wrap>
          <Typography.Text strong>Выбранное ядро:</Typography.Text>
          <Tag color={status?.coreType === 'sing-box' ? 'gold' : 'blue'}>{selectedCore}</Tag>
          {enabled && (
            <Tag color={coreMismatch || conflict ? 'warning' : 'success'}>
              Шлюз: {ownerCore}
            </Tag>
          )}
          <Tag>TPROXY: {status?.port ?? 52345}</Tag>
        </Space>

        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          При включении панель сохраняет резервную копию конфигурации выбранного ядра и добавляет
          отдельный TPROXY-вход. Outbound и routing не подменяются: перехваченный трафик проходит
          через ваши обычные правила маршрутизации Xray или sing-box.
        </Typography.Paragraph>

        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          Linux должен перенаправлять TCP/UDP через TPROXY на порт {status?.port ?? 52345} и иметь
          включённый IPv4 forwarding. Клиентские устройства должны использовать этот сервер как
          шлюз.
        </Typography.Paragraph>

        <Space wrap>
          {coreMismatch && !conflict && canEnable && (
            <Popconfirm
              title={`Перенести режим шлюза на ${selectedCore}?`}
              description="Панель удалит собственную конфигурацию режима шлюза из предыдущего ядра и включит её для выбранного ядра. При ошибке будет выполнен откат."
              okText="Перенести"
              cancelText="Отмена"
              onConfirm={() => void runAction('enable')}
            >
              <Button type="primary" loading={busy} disabled={loading}>
                Перенести на {selectedCore}
              </Button>
            </Popconfirm>
          )}

          {enabled ? (
            <Popconfirm
              title="Выключить режим шлюза?"
              description={
                recoveryOnly
                  ? 'Будет удалено только устаревшее состояние восстановления. Текущая конфигурация ядра не будет откатана.'
                  : 'Будут удалены только собственные TPROXY-объекты режима шлюза. Остальные настройки Xray и sing-box сохранятся.'
              }
              okText="Выключить"
              cancelText="Отмена"
              onConfirm={() => void runAction('disable')}
            >
              <Button danger loading={busy}>
                Выключить режим шлюза
              </Button>
            </Popconfirm>
          ) : (
            <Popconfirm
              title="Включить режим шлюза?"
              description={`Панель сохранит резервную копию и добавит TPROXY-вход для ${selectedCore}, не меняя ваши outbound и routing.`}
              okText="Включить"
              cancelText="Отмена"
              onConfirm={() => void runAction('enable')}
              disabled={!canEnable || loading}
            >
              <Button type="primary" loading={busy} disabled={!canEnable || loading}>
                Включить режим шлюза
              </Button>
            </Popconfirm>
          )}
          <Button icon={<ReloadOutlined />} disabled={busy} onClick={() => void refresh()}>
            Обновить
          </Button>
        </Space>
      </Space>
    </>
  );
}
