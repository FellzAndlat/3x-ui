import { Alert, Collapse, Input, InputNumber, Select, Switch } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';
import { useTranslation } from 'react-i18next';
import { FormField } from '@/components/form/rhf';

export default function SudokuFields() {
  const { t } = useTranslation();
  const { control } = useFormContext();
  const action = useWatch({ control, name: 'settings.suspiciousAction' });
  const maskDisabled = useWatch({ control, name: 'settings.httpmask.disable' });
  const maskMode = useWatch({ control, name: 'settings.httpmask.mode' });
  return (
    <>
      <Alert type="info" showIcon description={t('pages.inbounds.form.sudokuHint')} />
      <FormField
        name={['settings', 'key']}
        label={t('pages.inbounds.form.sudokuKey')}
        tooltip={t('pages.inbounds.form.sudokuKeyHint')}
      >
        <Input readOnly placeholder={t('pages.inbounds.form.sudokuKeyPending')} />
      </FormField>
      <FormField
        name={['settings', 'aead']}
        label={t('pages.inbounds.form.sudokuAead')}
        tooltip={t('pages.inbounds.form.sudokuAeadHint')}
      >
        <Select
          options={[
            { value: 'chacha20-poly1305', label: 'ChaCha20-Poly1305' },
            { value: 'aes-128-gcm', label: 'AES-128-GCM' },
            { value: 'none', label: t('pages.inbounds.form.sudokuOff') },
          ]}
        />
      </FormField>
      <FormField
        name={['settings', 'suspiciousAction']}
        label={t('pages.inbounds.form.sudokuSuspiciousAction')}
        tooltip={t('pages.inbounds.form.sudokuSuspiciousActionHint')}
      >
        <Select
          options={[
            { value: 'silent', label: t('pages.inbounds.form.sudokuSilent') },
            { value: 'fallback', label: t('pages.inbounds.form.sudokuFallback') },
          ]}
        />
      </FormField>
      {action === 'fallback' && (
        <FormField
          name={['settings', 'fallbackAddress']}
          label={t('pages.inbounds.form.sudokuFallbackAddress')}
          tooltip={t('pages.inbounds.form.sudokuFallbackAddressHint')}
        >
          <Input placeholder="127.0.0.1:80" />
        </FormField>
      )}
      <FormField
        name={['settings', 'httpmask', 'disable']}
        label={t('pages.inbounds.form.sudokuHttpMask')}
        tooltip={t('pages.inbounds.form.sudokuHttpMaskHint')}
        valueProp="checked"
      >
        <Switch />
      </FormField>
      {!maskDisabled && (
        <>
          <FormField
            name={['settings', 'httpmask', 'mode']}
            label={t('pages.inbounds.form.sudokuHttpMode')}
            tooltip={t('pages.inbounds.form.sudokuHttpModeHint')}
          >
            <Select
              options={[
                { value: 'auto', label: t('pages.inbounds.form.sudokuAuto') },
                { value: 'legacy', label: t('pages.inbounds.form.sudokuLegacy') },
                { value: 'stream', label: t('pages.inbounds.form.sudokuStream') },
                { value: 'poll', label: t('pages.inbounds.form.sudokuPoll') },
                { value: 'ws', label: 'WebSocket' },
              ]}
            />
          </FormField>
          {maskMode !== 'legacy' && (
            <>
              <FormField
                name={['settings', 'httpmask', 'tls']}
                label={t('pages.inbounds.form.sudokuTls')}
                tooltip={t('pages.inbounds.form.sudokuTlsHint')}
                valueProp="checked"
              >
                <Switch />
              </FormField>
              <FormField
                name={['settings', 'httpmask', 'host']}
                label={t('pages.inbounds.form.sudokuHost')}
                tooltip={t('pages.inbounds.form.sudokuHostHint')}
              >
                <Input placeholder="www.example.com" />
              </FormField>
            </>
          )}
          <FormField
            name={['settings', 'httpmask', 'pathRoot']}
            label={t('pages.inbounds.form.sudokuPathRoot')}
            tooltip={t('pages.inbounds.form.sudokuPathRootHint')}
          >
            <Input placeholder="abc123" />
          </FormField>
        </>
      )}
      <Collapse
        ghost
        items={[
          {
            key: 'advanced',
            label: t('pages.inbounds.form.sudokuAdvanced'),
            children: (
              <>
                <FormField
                  name={['settings', 'paddingMin']}
                  label={t('pages.inbounds.form.sudokuPaddingMin')}
                  tooltip={t('pages.inbounds.form.sudokuPaddingHint')}
                >
                  <InputNumber min={0} max={100} addonAfter="%" style={{ width: '100%' }} />
                </FormField>
                <FormField
                  name={['settings', 'paddingMax']}
                  label={t('pages.inbounds.form.sudokuPaddingMax')}
                  tooltip={t('pages.inbounds.form.sudokuPaddingHint')}
                >
                  <InputNumber min={0} max={100} addonAfter="%" style={{ width: '100%' }} />
                </FormField>
                <FormField
                  name={['settings', 'ascii']}
                  label={t('pages.inbounds.form.sudokuAscii')}
                  tooltip={t('pages.inbounds.form.sudokuAsciiHint')}
                >
                  <Select
                    options={[
                      { value: 'prefer_entropy', label: t('pages.inbounds.form.sudokuEntropy') },
                      { value: 'prefer_ascii', label: t('pages.inbounds.form.sudokuAsciiOnly') },
                      {
                        value: 'up_ascii_down_entropy',
                        label: t('pages.inbounds.form.sudokuUpAscii'),
                      },
                      {
                        value: 'up_entropy_down_ascii',
                        label: t('pages.inbounds.form.sudokuDownAscii'),
                      },
                    ]}
                  />
                </FormField>
                <FormField
                  name={['settings', 'customTable']}
                  label={t('pages.inbounds.form.sudokuCustomTable')}
                  tooltip={t('pages.inbounds.form.sudokuCustomTableHint')}
                >
                  <Input placeholder="xpxvvpvv" />
                </FormField>
                <FormField
                  name={['settings', 'customTables']}
                  label={t('pages.inbounds.form.sudokuCustomTables')}
                  tooltip={t('pages.inbounds.form.sudokuCustomTablesHint')}
                >
                  <Select mode="tags" tokenSeparators={[',', ';']} style={{ width: '100%' }} />
                </FormField>
                <FormField
                  name={['settings', 'enablePureDownlink']}
                  label={t('pages.inbounds.form.sudokuPureDownlink')}
                  tooltip={t('pages.inbounds.form.sudokuPureDownlinkHint')}
                  valueProp="checked"
                >
                  <Switch />
                </FormField>
                <FormField
                  name={['settings', 'multiplex']}
                  label={t('pages.inbounds.form.sudokuMultiplex')}
                  tooltip={t('pages.inbounds.form.sudokuMultiplexHint')}
                >
                  <Select
                    options={[
                      { value: 'off', label: t('pages.inbounds.form.sudokuOff') },
                      { value: 'auto', label: t('pages.inbounds.form.sudokuAuto') },
                      { value: 'on', label: t('pages.inbounds.form.sudokuOn') },
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
