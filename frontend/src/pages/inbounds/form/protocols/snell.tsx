import { Alert, Button, Input, Select } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { FormField } from '@/components/form/rhf';

export default function SnellFields() {
  const { t } = useTranslation();
  const { control, setValue } = useFormContext();
  const version = useWatch({ control, name: 'settings.version' });
  function preset(version: 5 | 6) {
    setValue('settings.version', version, { shouldDirty: true });
    setValue('settings.mode', 'default', { shouldDirty: true });
    setValue('settings.obfsMode', version === 5 ? 'http' : 'none', { shouldDirty: true });
    setValue('settings.obfsHost', 'bing.com', { shouldDirty: true });
  }
  return (
    <>
      <Alert type="info" showIcon description={t('pages.inbounds.form.snellHint')} />
      <Button onClick={() => preset(6)}>{t('pages.inbounds.form.snellAuto')}</Button>
      <Button onClick={() => preset(5)}>{t('pages.inbounds.form.snellCompatible')}</Button>
      <FormField name={['settings', 'version']} label="Snell">
        <Select
          options={[
            { value: 6, label: 'v6' },
            { value: 5, label: 'v5 / v4' },
          ]}
          onChange={(v) => preset(v)}
        />
      </FormField>
      <FormField name={['settings', 'psk']} label="PSK">
        <Input.Password placeholder={t('pages.inbounds.form.additionalSecretAuto')} />
      </FormField>
      {version === 6 ? (
        <FormField name={['settings', 'mode']} label={t('pages.inbounds.form.snellMode')}>
          <Select
            options={[
              { value: 'default', label: 'default' },
              { value: 'unshaped', label: 'unshaped' },
            ]}
          />
        </FormField>
      ) : (
        <>
          <FormField name={['settings', 'obfsMode']} label="Obfs">
            <Select
              options={[
                { value: 'none', label: 'none' },
                { value: 'http', label: 'HTTP' },
              ]}
            />
          </FormField>
          <FormField name={['settings', 'obfsHost']} label="HTTP Host">
            <Input />
          </FormField>
        </>
      )}
    </>
  );
}
