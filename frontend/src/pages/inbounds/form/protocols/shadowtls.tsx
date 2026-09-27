import { useTranslation } from 'react-i18next';
import { Collapse, Input, InputNumber, Select, Switch, Typography } from 'antd';
import { FormField } from '@/components/form/rhf';

export default function ShadowTlsFields({
  prefix = '',
  showPassword = false,
}: {
  prefix?: string;
  showPassword?: boolean;
}) {
  const { t } = useTranslation();
  const field = (name: string) => ['settings', ...(prefix ? [prefix] : []), ...name.split('.')];

  return (
    <>
      <FormField
        name={field('handshake.server')}
        label={t('pages.inbounds.form.shadowTlsHandshakeServer')}
        tooltip={t('pages.inbounds.form.shadowTlsHandshakeServerHint')}
      >
        <Input placeholder="cloudflare.com" />
      </FormField>

      <Typography.Paragraph type="secondary">
        {t('pages.inbounds.form.shadowTlsHint')}
      </Typography.Paragraph>
      <Collapse
        ghost
        items={[
          {
            key: 'advanced',
            label: t('pages.inbounds.form.shadowTlsAdvanced'),
            children: (
              <>
                {showPassword && (
                  <FormField
                    name={field('password')}
                    label={t('pages.inbounds.form.shadowTlsPassword')}
                  >
                    <Input.Password autoComplete="off" />
                  </FormField>
                )}
                <FormField
                  name={field('handshake.serverPort')}
                  label={t('pages.inbounds.form.shadowTlsHandshakePort')}
                  tooltip={t('pages.inbounds.form.shadowTlsHandshakePortHint')}
                >
                  <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                </FormField>
                <FormField
                  name={field('strictMode')}
                  label={t('pages.inbounds.form.shadowTlsStrictMode')}
                  tooltip={t('pages.inbounds.form.shadowTlsStrictModeHint')}
                  valueProp="checked"
                >
                  <Switch />
                </FormField>
                <FormField
                  name={field('wildcardSni')}
                  label={t('pages.inbounds.form.shadowTlsWildcardSni')}
                  tooltip={t('pages.inbounds.form.shadowTlsWildcardSniHint')}
                >
                  <Select
                    options={[
                      { label: t('pages.inbounds.form.shadowTlsWildcardSniOff'), value: 'off' },
                      {
                        label: t('pages.inbounds.form.shadowTlsWildcardSniAuthed'),
                        value: 'authed',
                      },
                      { label: t('pages.inbounds.form.shadowTlsWildcardSniAll'), value: 'all' },
                    ]}
                  />
                </FormField>
              </>
            ),
          },
        ]}
      />
    </>
  );
}
