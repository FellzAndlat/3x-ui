import { useState } from 'react';
import { Alert, Button, Collapse, Form, Input, InputNumber, Select, Space, message } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { FormField } from '@/components/form/rhf';
import { HttpUtil } from '@/utils';

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
  const { control, setValue } = useFormContext();
  const [loadingPanelCert, setLoadingPanelCert] = useState(false);
  const shareAddr = (useWatch({ control, name: 'shareAddr' }) ?? '') as string;
  const nodeId = useWatch({ control, name: 'nodeId' }) as number | null | undefined;
  const certificate = (useWatch({ control, name: 'settings.certificate' }) ?? '') as string;
  const privateKey = (useWatch({ control, name: 'settings.privateKey' }) ?? '') as string;

  const updateSetting = (field: string, value: string) => {
    setValue(`settings.${field}`, value, {
      shouldDirty: true,
      shouldTouch: true,
      shouldValidate: true,
    });
  };

  const autofillHostname = () => {
    const candidate = (shareAddr || window.location.hostname || '').trim().replace(/^\[|\]$/g, '');
    if (!candidate || candidate === '0.0.0.0' || candidate === '::') {
      message.warning(t('pages.inbounds.form.trusttunnelHostnameHint'));
      return;
    }
    updateSetting('hostname', candidate);
    message.success(t('pages.inbounds.setSuccess'));
  };

  const usePanelCertificate = async () => {
    setLoadingPanelCert(true);
    try {
      const response =
        typeof nodeId === 'number'
          ? await HttpUtil.get(`/panel/api/nodes/webCert/${nodeId}`, undefined, { silent: true })
          : await HttpUtil.post('/panel/api/setting/all', undefined, { silent: true });
      if (!response?.success) {
        message.warning(response?.msg || t('pages.inbounds.setDefaultCertEmpty'));
        return;
      }
      const obj = response.obj as { webCertFile?: string; webKeyFile?: string };
      if (!obj?.webCertFile || !obj?.webKeyFile) {
        message.warning(t('pages.inbounds.setDefaultCertEmpty'));
        return;
      }
      updateSetting('certificate', obj.webCertFile);
      updateSetting('privateKey', obj.webKeyFile);
      message.success(t('pages.inbounds.setSuccess'));
    } catch {
      message.error(t('somethingWentWrong'));
    } finally {
      setLoadingPanelCert(false);
    }
  };

  const useAutomaticCertificate = () => {
    updateSetting('certificate', '');
    updateSetting('privateKey', '');
    message.success(t('pages.inbounds.setSuccess'));
  };

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
      <Space wrap style={{ marginTop: -12, marginBottom: 12 }}>
        <Button onClick={autofillHostname}>
          {t('pages.inbounds.form.trusttunnelAuto', { defaultValue: 'Auto' })}
        </Button>
      </Space>

      <Alert
        type="success"
        showIcon
        message={
          certificate && privateKey
            ? t('pages.inbounds.form.trusttunnelCustomCertificate', {
                defaultValue: 'TLS: certificate from panel/custom files',
              })
            : t('pages.inbounds.form.trusttunnelAutomaticCertificate', {
                defaultValue: 'TLS: automatic certificate. No manual configuration is required.',
              })
        }
        style={{ marginBottom: 12 }}
      />

      <Collapse
        ghost
        items={[
          {
            key: 'tls',
            label: t('pages.inbounds.advancedTitle'),
            children: (
              <>
                <Space wrap style={{ marginBottom: 12 }}>
                  <Button loading={loadingPanelCert} onClick={usePanelCertificate}>
                    {t('pages.inbounds.setDefaultCert')}
                  </Button>
                  <Button onClick={useAutomaticCertificate}>
                    {t('pages.inbounds.form.trusttunnelAutomaticCertificate', {
                      defaultValue: 'Automatic certificate',
                    })}
                  </Button>
                </Space>
                <FormField
                  name={['settings', 'certificate']}
                  label={t('pages.inbounds.form.trusttunnelCert')}
                  tooltip={t('pages.inbounds.form.trusttunnelCertHint')}
                >
                  <Input placeholder="/path/to/fullchain.pem" />
                </FormField>
                <FormField
                  name={['settings', 'privateKey']}
                  label={t('pages.inbounds.form.trusttunnelKey')}
                  tooltip={t('pages.inbounds.form.trusttunnelKeyHint')}
                >
                  <Input.Password placeholder="/path/to/privkey.pem" />
                </FormField>
              </>
            ),
          },
        ]}
      />
    </>
  );
}
