import { Alert, Collapse, Input } from 'antd';
import { useTranslation } from 'react-i18next';
import { FormField } from '@/components/form/rhf';
import { ANYTLS_DEFAULT_PADDING_SCHEME } from '@/schemas/protocols/inbound/anytls';

export default function AnyTlsFields() {
  const { t } = useTranslation();

  return (
    <>
      <Alert type="info" showIcon description={t('pages.inbounds.form.anytlsHint')} />
      <FormField
        name={['settings', 'tls', 'serverName']}
        label={t('pages.inbounds.form.anytlsServerName')}
        tooltip={t('pages.inbounds.form.anytlsServerNameHint')}
      >
        <Input placeholder="vpn.example.com" />
      </FormField>
      <FormField
        name={['settings', 'tls', 'certificatePath']}
        label={t('pages.inbounds.form.anytlsCertificate')}
        tooltip={t('pages.inbounds.form.anytlsCertificateHint')}
      >
        <Input placeholder="/etc/letsencrypt/live/vpn.example.com/fullchain.pem" />
      </FormField>
      <FormField
        name={['settings', 'tls', 'keyPath']}
        label={t('pages.inbounds.form.anytlsPrivateKey')}
        tooltip={t('pages.inbounds.form.anytlsPrivateKeyHint')}
      >
        <Input placeholder="/etc/letsencrypt/live/vpn.example.com/privkey.pem" />
      </FormField>
      <Collapse
        ghost
        items={[
          {
            key: 'padding',
            label: t('pages.inbounds.form.anytlsAdvanced'),
            children: (
              <FormField
                name={['settings', 'paddingScheme']}
                label={t('pages.inbounds.form.anytlsPaddingScheme')}
                tooltip={t('pages.inbounds.form.anytlsPaddingSchemeHint')}
                transform={{
                  input: (value) =>
                    Array.isArray(value)
                      ? value.join('\n')
                      : ANYTLS_DEFAULT_PADDING_SCHEME.join('\n'),
                  output: (value) =>
                    String(value ?? '')
                      .split(/\r?\n/)
                      .map((line) => line.trim())
                      .filter(Boolean),
                }}
              >
                <Input.TextArea rows={8} spellCheck={false} />
              </FormField>
            ),
          },
        ]}
      />
    </>
  );
}
