import { Alert, Button, Input, InputNumber, Select } from 'antd';
import { useFormContext } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { FormField } from '@/components/form/rhf';
import { MasqueInboundSettingsSchema } from '@/schemas/protocols/inbound/masque';

export default function MasqueFields() {
  const { t } = useTranslation();
  const { setValue } = useFormContext();
  return (
    <>
      <Alert type="info" showIcon description={t('pages.inbounds.form.masqueHint')} />
      <Button
        onClick={() => {
          const defaults = MasqueInboundSettingsSchema.parse({});
          for (const key of ['version', 'path', 'address', 'advertiseRoutes', 'mtu'] as const) {
            setValue(`settings.${key}`, defaults[key], { shouldDirty: true });
          }
        }}
      >
        {t('pages.inbounds.form.masqueAuto')}
      </Button>
      <FormField name={['settings', 'version']} label={t('pages.inbounds.form.masqueVersion')}>
        <Select
          mode="multiple"
          options={[
            { value: 3, label: 'HTTP/3 (QUIC)' },
            { value: 2, label: 'HTTP/2 (TLS)' },
            { value: 1, label: 'HTTP/1.1 (TLS)' },
          ]}
        />
      </FormField>
      <FormField name={['settings', 'path']} label={t('pages.inbounds.form.masquePath')}>
        <Input />
      </FormField>
      <FormField name={['settings', 'address']} label={t('pages.inbounds.form.masqueAddress')}>
        <Select mode="tags" />
      </FormField>
      <FormField
        name={['settings', 'advertiseRoutes']}
        label={t('pages.inbounds.form.masqueRoutes')}
      >
        <Select mode="tags" placeholder="0.0.0.0/0, ::/0" />
      </FormField>
      <FormField name={['settings', 'mtu']} label="MTU">
        <InputNumber min={1280} max={65535} />
      </FormField>
      <FormField name={['settings', 'tls', 'serverName']} label="SNI">
        <Input placeholder="example.com" />
      </FormField>
      <FormField
        name={['settings', 'tls', 'certificatePath']}
        label={t('pages.inbounds.form.masqueCertificate')}
      >
        <Input />
      </FormField>
      <FormField name={['settings', 'tls', 'keyPath']} label={t('pages.inbounds.form.masqueKey')}>
        <Input />
      </FormField>
    </>
  );
}
