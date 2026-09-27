import { useTranslation } from 'react-i18next';
import { Collapse, InputNumber, Select, Switch } from 'antd';

import { FormField } from '@/components/form/rhf';

const loggingOptions = ['FATAL', 'ERROR', 'WARN', 'INFO', 'DEBUG', 'TRACE'].map((value) => ({
  value,
  label: value,
}));

const multiplexingValues = [
  'MULTIPLEXING_OFF',
  'MULTIPLEXING_LOW',
  'MULTIPLEXING_MIDDLE',
  'MULTIPLEXING_HIGH',
] as const;

const handshakeValues = ['HANDSHAKE_STANDARD', 'HANDSHAKE_NO_WAIT'] as const;

export default function MieruFields() {
  const { t } = useTranslation();

  return (
    <>
      <div
        style={{
          color: 'var(--ant-color-text-secondary)',
          fontSize: 12,
          marginBottom: 8,
        }}
      >
        {t('pages.inbounds.form.mieruAutoHint')}
      </div>

      <FormField
        name={['settings', 'tcpPorts']}
        label={t('pages.inbounds.form.mieruTcpPorts')}
        tooltip={t('pages.inbounds.form.mieruPortRangesHint')}
      >
        <Select
          mode="tags"
          tokenSeparators={[',', ';']}
          placeholder="2012-2022"
          style={{ width: '100%' }}
        />
      </FormField>

      <FormField
        name={['settings', 'udpPorts']}
        label={t('pages.inbounds.form.mieruUdpPorts')}
        tooltip={t('pages.inbounds.form.mieruPortRangesHint')}
      >
        <Select
          mode="tags"
          tokenSeparators={[',', ';']}
          placeholder="2023-2033"
          style={{ width: '100%' }}
        />
      </FormField>

      <Collapse
        ghost
        items={[
          {
            key: 'advanced',
            label: t('pages.inbounds.form.mieruClientSettings'),
            children: (
              <>
                <FormField
                  name={['settings', 'multiplexing']}
                  label={t('pages.inbounds.form.mieruMultiplexing')}
                  tooltip={t('pages.inbounds.form.mieruMultiplexingHint')}
                >
                  <Select
                    options={multiplexingValues.map((value) => ({
                      value,
                      label: t(`pages.inbounds.form.mieruMultiplexingOptions.${value}`),
                    }))}
                  />
                </FormField>

                <FormField
                  name={['settings', 'handshakeMode']}
                  label={t('pages.inbounds.form.mieruHandshakeMode')}
                  tooltip={t('pages.inbounds.form.mieruHandshakeModeHint')}
                >
                  <Select
                    options={handshakeValues.map((value) => ({
                      value,
                      label: t(`pages.inbounds.form.mieruHandshakeOptions.${value}`),
                    }))}
                  />
                </FormField>

                <FormField name={['settings', 'mtu']} label={t('pages.inbounds.form.mieruMtu')}>
                  <InputNumber min={1280} max={1400} style={{ width: '100%' }} />
                </FormField>

                <FormField
                  name={['settings', 'loggingLevel']}
                  label={t('pages.inbounds.form.mieruLoggingLevel')}
                >
                  <Select options={loggingOptions} />
                </FormField>

                <FormField
                  name={['settings', 'userHintIsMandatory']}
                  label={t('pages.inbounds.form.mieruUserHintMandatory')}
                  valueProp="checked"
                >
                  <Switch />
                </FormField>
              </>
            ),
          },
        ]}
      />
    </>
  );
}
