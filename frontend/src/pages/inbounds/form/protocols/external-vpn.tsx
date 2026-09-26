import { Alert, Form, Input, InputNumber, Select } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { FormField } from '@/components/form/rhf';

export function PingtunnelFields() {
  const { t } = useTranslation();
  const { control } = useFormContext();
  const key = useWatch({ control, name: 'settings.key' }) as number | undefined;
  const secret = useWatch({ control, name: 'settings.encryptKey' }) as string | undefined;
  const encrypt = useWatch({ control, name: 'settings.encrypt' }) as string | undefined;
  const address = useWatch({ control, name: 'shareAddr' }) as string | undefined;
  return (
    <>
      <Alert type="info" showIcon description={t('pages.inbounds.form.pingtunnelHint')} />
      <FormField name={['settings', 'key']} label={t('pages.inbounds.form.pingtunnelKey')}>
        <InputNumber min={0} max={2147483647} />
      </FormField>
      <FormField name={['settings', 'encrypt']} label={t('pages.inbounds.form.pingtunnelEncrypt')}>
        <Select
          options={[
            { value: 'chacha20', label: 'ChaCha20-Poly1305' },
            { value: 'aes256', label: 'AES-256-GCM' },
            { value: 'aes128', label: 'AES-128-GCM' },
            { value: '', label: t('pages.inbounds.form.pingtunnelNoEncryption') },
          ]}
        />
      </FormField>
      {encrypt !== '' && (
        <FormField
          name={['settings', 'encryptKey']}
          label={t('pages.inbounds.form.pingtunnelSecret')}
        >
          <Input.Password autoComplete="new-password" />
        </FormField>
      )}
      <FormField name={['settings', 'maxConn']} label={t('pages.inbounds.form.pingtunnelMaxConn')}>
        <InputNumber min={0} />
      </FormField>
      <FormField
        name={['settings', 'connectTimeout']}
        label={t('pages.inbounds.form.pingtunnelConnectTimeout')}
      >
        <InputNumber min={0} addonAfter="ms" />
      </FormField>
      <FormField name={['settings', 'forward']} label={t('pages.inbounds.form.pingtunnelForward')}>
        <Input placeholder="socks5://127.0.0.1:2080" />
      </FormField>
      <FormField
        name={['settings', 'congestion']}
        label={t('pages.inbounds.form.pingtunnelCongestion')}
      >
        <Select
          options={[
            { value: 'bb', label: 'bb' },
            { value: 'none', label: t('pages.inbounds.form.pingtunnelNoCongestion') },
          ]}
        />
      </FormField>
      {(key ?? 0) > 0 && (encrypt === '' || !!secret) && (
        <Form.Item label={t('pages.inbounds.form.pingtunnelClientConfig')}>
          <Input.TextArea
            readOnly
            autoSize
            value={JSON.stringify(
              {
                type: 'client',
                listen: '127.0.0.1:1080',
                server: address || 'PUBLIC_SERVER_IP',
                sock5: 1,
                key,
                ...(encrypt ? { encrypt, encrypt_key: secret } : {}),
              },
              null,
              2,
            )}
          />
        </Form.Item>
      )}
    </>
  );
}

export function TrustTunnelFields() {
  const { t } = useTranslation();
  return (
    <>
      <Alert type="info" showIcon description={t('pages.inbounds.form.trusttunnelHint')} />
      <FormField
        name={['settings', 'hostname']}
        label={t('pages.inbounds.form.trusttunnelHostname')}
        tooltip={t('pages.inbounds.form.trusttunnelHostnameHint')}
      >
        <Input placeholder="vpn.example.com" />
      </FormField>
      <FormField
        name={['settings', 'certificate']}
        label={t('pages.inbounds.form.trusttunnelCert')}
        tooltip={t('pages.inbounds.form.trusttunnelCertHint')}
      >
        <Input />
      </FormField>
      <FormField
        name={['settings', 'privateKey']}
        label={t('pages.inbounds.form.trusttunnelKey')}
        tooltip={t('pages.inbounds.form.trusttunnelKeyHint')}
      >
        <Input.Password />
      </FormField>
    </>
  );
}
