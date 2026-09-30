import { useCallback, useEffect, useLayoutEffect, useState } from 'react';
import { Space, Switch, Tooltip, Typography, message } from 'antd';
import { createPortal } from 'react-dom';

import { HttpUtil } from '@/utils';

type SubscriptionProxySetting = {
  enabled: boolean;
};

const jsonOptions = { headers: { 'Content-Type': 'application/json' } };

export default function TelemtSubscriptionToggle() {
  const [portalTarget, setPortalTarget] = useState<Element | null>(null);
  const [enabled, setEnabled] = useState(false);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useLayoutEffect(() => {
    setPortalTarget(document.querySelector('.telemt-header'));
  }, []);

  const loadSetting = useCallback(async () => {
    setLoading(true);
    try {
      const response = await HttpUtil.get<SubscriptionProxySetting>(
        '/panel/api/telemt/subscription-proxy',
      );
      if (!response?.success || !response.obj) {
        setLoaded(false);
        message.error(response?.msg || 'Не удалось получить настройку личных Telemt-прокси');
        return;
      }
      setEnabled(Boolean(response.obj.enabled));
      setLoaded(true);
    } catch {
      setLoaded(false);
      message.error('Не удалось получить настройку личных Telemt-прокси');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void loadSetting();
  }, [loadSetting]);

  const updateSetting = async (checked: boolean) => {
    if (!loaded || saving) return;

    const previous = enabled;
    setEnabled(checked);
    setSaving(true);
    try {
      const response = await HttpUtil.post<SubscriptionProxySetting>(
        '/panel/api/telemt/subscription-proxy',
        { enabled: checked },
        jsonOptions,
      );
      if (!response?.success || !response.obj) {
        setEnabled(previous);
        message.error(response?.msg || 'Не удалось сохранить настройку личных Telemt-прокси');
        return;
      }
      setEnabled(Boolean(response.obj.enabled));
      message.success(
        response.obj.enabled
          ? 'Личные Telemt-прокси для подписок включены'
          : 'Личные Telemt-прокси для подписок выключены',
      );
    } catch {
      setEnabled(previous);
      message.error('Не удалось сохранить настройку личных Telemt-прокси');
    } finally {
      setSaving(false);
    }
  };

  if (!portalTarget) return null;

  return createPortal(
    <Tooltip
      title={
        !loaded
          ? 'Состояние настройки не удалось получить. Обновите страницу и повторите попытку.'
          : enabled
            ? 'Для подписок создаются личные Telemt-прокси. Уже созданные прокси сохраняются.'
            : 'Новые личные Telemt-прокси не создаются, вкладка Telemt в подписке скрыта. Существующие прокси не удаляются.'
      }
    >
      <Space size={8} wrap>
        <Typography.Text type="secondary">Личные прокси в подписках</Typography.Text>
        <Switch
          aria-label="Личные Telemt-прокси в подписках"
          checked={enabled}
          loading={loading || saving}
          disabled={!loaded || loading || saving}
          onChange={(checked) => void updateSetting(checked)}
        />
      </Space>
    </Tooltip>,
    portalTarget,
  );
}
