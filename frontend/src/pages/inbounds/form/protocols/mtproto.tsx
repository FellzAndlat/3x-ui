import { useTranslation } from 'react-i18next';
import { Divider, Input, InputNumber, Select, Switch, Typography } from 'antd';
import { useFormContext, useWatch } from 'react-hook-form';

import { FormField } from '@/components/form/rhf';
import { useOutboundTags } from '@/api/queries/useOutboundTags';

export default function MtprotoFields() {
  const { t } = useTranslation();
  const { control } = useFormContext();
  const routeThroughXray = useWatch({ control, name: 'settings.routeThroughXray' }) as
    | boolean
    | undefined;
  const mekoEnabled = useWatch({ control, name: 'settings.mekoFix.enabled' }) as
    | boolean
    | undefined;
  const proxyProtocolEnabled = useWatch({ control, name: 'settings.proxyProtocolListener' }) as
    | boolean
    | undefined;
  const { data: outboundTags } = useOutboundTags({ excludeBlackhole: true });

  return (
    <>
      <Typography.Text strong>Telemt</Typography.Text>
      <FormField
        name={['settings', 'fakeTlsDomain']}
        label={t('pages.inbounds.form.fakeTlsDomain')}
        tooltip={t('pages.inbounds.form.mtprotoFakeTlsDomainHint')}
      >
        <Input placeholder="www.cloudflare.com" />
      </FormField>
      <FormField
        name={['settings', 'tlsDomains']}
        label="Additional FakeTLS domains"
        tooltip="Telemt will generate an additional FakeTLS link for every domain."
      >
        <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="example.com" />
      </FormField>

      <FormField name={['settings', 'telemtModes', 'classic']} label="Classic mode" valueProp="checked">
        <Switch />
      </FormField>
      <FormField name={['settings', 'telemtModes', 'secure']} label="Secure (DD) mode" valueProp="checked">
        <Switch />
      </FormField>
      <FormField name={['settings', 'telemtModes', 'tls']} label="FakeTLS (EE) mode" valueProp="checked">
        <Switch />
      </FormField>
      <FormField
        name={['settings', 'mask']}
        label="Traffic masking"
        tooltip="Telemt censorship.mask. Masks invalid/non-MTProto traffic."
        valueProp="checked"
      >
        <Switch />
      </FormField>
      <FormField
        name={['settings', 'tlsEmulation']}
        label="TLS emulation"
        tooltip="Telemt censorship.tls_emulation. Emulates TLS record/certificate behavior."
        valueProp="checked"
      >
        <Switch />
      </FormField>
      <FormField
        name={['settings', 'proxyProtocolListener']}
        label="PROXY protocol listener"
        valueProp="checked"
      >
        <Switch />
      </FormField>
      {proxyProtocolEnabled && (
        <FormField
          name={['settings', 'proxyProtocolTrustedCidrs']}
          label="Trusted PROXY protocol CIDRs"
          tooltip="Only PROXY headers from these source networks are accepted. Loopback is the safe default for the local Xray bridge."
        >
          <Select mode="tags" tokenSeparators={[',', ' ']} placeholder="127.0.0.0/8" />
        </FormField>
      )}
      <FormField name={['settings', 'preferIp']} label="Telemt IP preference">
        <Select
          allowClear
          options={[
            { value: 'prefer-ipv6', label: 'Prefer IPv6' },
            { value: 'prefer-ipv4', label: 'Prefer IPv4' },
            { value: 'only-ipv6', label: 'IPv6 only' },
            { value: 'only-ipv4', label: 'IPv4 only' },
          ]}
        />
      </FormField>
      <FormField name={['settings', 'debug']} label="Telemt debug logging" valueProp="checked">
        <Switch />
      </FormField>

      <Divider orientation="left">MEKO proxy fix</Divider>
      <FormField
        name={['settings', 'mekoFix', 'enabled']}
        label="Enable MTPROTO_FIX_By_MEKO"
        tooltip="Installs an inbound-scoped SYN filter for this Telemt port."
        valueProp="checked"
      >
        <Switch />
      </FormField>
      {mekoEnabled !== false && (
        <>
          <FormField name={['settings', 'mekoFix', 'backend']} label="Firewall backend">
            <Select
              options={[
                { value: 'auto', label: 'Auto (nftables → iptables)' },
                { value: 'nftables', label: 'nftables' },
                { value: 'iptables', label: 'iptables' },
              ]}
            />
          </FormField>
          <FormField
            name={['settings', 'mekoFix', 'synRatePerMinute']}
            label="Non-iOS SYN rate / minute"
            tooltip="MEKO v3 default is 54 SYN/minute per source IP."
          >
            <InputNumber min={1} max={100000} style={{ width: '100%' }} />
          </FormField>
          <FormField
            name={['settings', 'mekoFix', 'burst']}
            label="SYN burst"
            tooltip="MEKO v3 default is 1 packet."
          >
            <InputNumber min={1} max={10000} style={{ width: '100%' }} />
          </FormField>
          <FormField
            name={['settings', 'mekoFix', 'iosBypass']}
            label="iOS signature bypass"
            tooltip="Recognize MEKO's iOS TCP signature and bypass the non-iOS SYN limiter."
            valueProp="checked"
          >
            <Switch />
          </FormField>
        </>
      )}

      <Divider orientation="left">Routing</Divider>
      <FormField
        name={['settings', 'routeThroughXray']}
        label={t('pages.inbounds.form.mtgRouteThroughXray')}
        tooltip="Route Telemt upstream connections through the existing local Xray SOCKS bridge."
        valueProp="checked"
      >
        <Switch />
      </FormField>
      {routeThroughXray && (
        <FormField
          name={['settings', 'outboundTag']}
          label={t('pages.inbounds.form.mtgRouteOutbound')}
        >
          <Select
            id="mtprotoOutboundTag"
            allowClear
            showSearch
            options={(outboundTags ?? []).map((tag) => ({ value: tag, label: tag }))}
          />
        </FormField>
      )}
      <FormField
        name={['settings', 'publicIpv4']}
        label={t('pages.inbounds.form.mtgPublicIpv4')}
      >
        <Input allowClear placeholder="1.2.3.4" />
      </FormField>
      <FormField
        name={['settings', 'publicIpv6']}
        label={t('pages.inbounds.form.mtgPublicIpv6')}
      >
        <Input allowClear placeholder="2001:db8::1" />
      </FormField>
    </>
  );
}
