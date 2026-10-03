import { Alert, Button, Form, Input, InputNumber, Select, Space } from 'antd';
import { useTranslation } from 'react-i18next';
import { InstallVPNButton } from '@/pages/inbounds/form/protocols/additional-vpn';

export default function AdditionalOutboundEditor({
  text,
  onChange,
}: {
  text: string;
  onChange: (text: string) => void;
}) {
  const { t } = useTranslation();
  let raw: Record<string, unknown>;
  try {
    raw = JSON.parse(text) as Record<string, unknown>;
  } catch {
    return null;
  }
  const protocol = raw?.protocol;
  if (!['fptn', 'openflux', 'snell', 'singbox:snell'].includes(String(protocol))) return null;
  const settings = (raw.settings ?? {}) as Record<string, unknown>;
  const update = (values: Record<string, unknown>) =>
    onChange(JSON.stringify({ ...raw, settings: { ...settings, ...values } }, null, 2));
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Alert type="info" showIcon description={t('pages.inbounds.form.vpnOutboundHint')} />
      {protocol === 'fptn' ? (
        <>
          <InstallVPNButton protocol="fptn" />
          <Form.Item label={t('pages.inbounds.form.fptnToken')}>
            <Input.TextArea
              value={String(settings.token ?? '')}
              onChange={(e) => update({ token: e.target.value })}
              autoSize
            />
          </Form.Item>
          <Form.Item label="MTU">
            <InputNumber
              value={Number(settings.mtu ?? 1400)}
              min={576}
              max={9000}
              onChange={(mtu) => update({ mtu })}
            />
          </Form.Item>
          <Form.Item label="SNI">
            <Input
              value={String(settings.sni ?? '')}
              onChange={(e) => update({ sni: e.target.value })}
            />
          </Form.Item>
          <Form.Item label={t('pages.inbounds.form.fptnBypass')}>
            <Select
              value={String(settings.bypass ?? 'sni-spoofing')}
              options={['sni-spoofing', 'obfuscation'].map((value) => ({ value, label: value }))}
              onChange={(bypass) => update({ bypass })}
            />
          </Form.Item>
          <Button
            onClick={() => update({ mtu: 1280, bypass: 'sni-spoofing', sni: 'www.bing.com' })}
          >
            {t('pages.inbounds.form.vpnMobile')}
          </Button>
        </>
      ) : protocol === 'openflux' ? (
        <>
          <InstallVPNButton protocol="openflux" />
          <Form.Item label={t('pages.inbounds.form.openfluxSecret')}>
            <Input.Password
              value={String(settings.secret ?? '')}
              onChange={(e) => update({ secret: e.target.value })}
            />
          </Form.Item>
          <Alert type="info" description={t('pages.inbounds.form.openfluxOutboundHint')} />
        </>
      ) : (
        <>
          <Form.Item label="Snell">
            <Select
              value={Number(settings.version ?? 4)}
              options={[
                { value: 4, label: 'v4 / v5' },
                { value: 6, label: 'v6' },
              ]}
              onChange={(version) =>
                update({
                  version,
                  mode: version === 6 ? 'default' : undefined,
                  obfs_mode: undefined,
                  obfs_host: undefined,
                })
              }
            />
          </Form.Item>
          <Form.Item label="PSK">
            <Input.Password
              value={String(settings.psk ?? '')}
              onChange={(e) => update({ psk: e.target.value })}
            />
          </Form.Item>
          <Form.Item label="User key">
            <Input.Password
              value={String(settings.userkey ?? '')}
              onChange={(e) => update({ userkey: e.target.value })}
            />
          </Form.Item>
          <Button
            onClick={() =>
              update({ version: 6, mode: 'default', obfs_mode: undefined, obfs_host: undefined })
            }
          >
            {t('pages.inbounds.form.snellAuto')}
          </Button>
        </>
      )}
    </Space>
  );
}
