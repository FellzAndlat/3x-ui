import { useCallback, useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import {
  Alert,
  Button,
  Card,
  ConfigProvider,
  Collapse,
  Divider,
  FloatButton,
  Input,
  InputNumber,
  Layout,
  Modal,
  Result,
  Row,
  Col,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tabs,
  Tag,
  Empty,
  message,
} from 'antd';
import {
  CodeOutlined,
  DeleteOutlined,
  ExportOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
} from '@ant-design/icons';

import AdBlockTab from '@/pages/adblock/AdBlockTab';
import AppSidebar from '@/layouts/AppSidebar';
import { HttpUtil } from '@/utils';
import { useTheme } from '@/hooks/useTheme';
import './SingBoxPage.css';
import SimpleSettings from './SimpleSettings';
import {
  outboundDefaults,
  outboundErrors,
  optimisticCacheEnabled,
  setDnsCache,
  setOptimisticCache,
} from './simple-settings';
import { parseShareLink, uniqueTag } from './share-links';

type SectionKey =
  | 'schema'
  | 'log'
  | 'dns'
  | 'ntp'
  | 'certificate'
  | 'certificate_providers'
  | 'http_clients'
  | 'network_namespaces'
  | 'endpoints'
  | 'outbounds'
  | 'route'
  | 'experimental';

type JsonObject = Record<string, unknown>;
type ConfigMap = JsonObject;
type ApiMsg<T = unknown> = { success?: boolean; msg?: string; obj?: T };

type Snapshot = {
  inboundTags?: string[];
  config: ConfigMap;
  running: boolean;
  version: string;
  configPath: string;
  configSource: 'disk' | 'generated' | string;
  configModified: string;
  managedSections: string[];
};

const sectionFallbacks: Record<SectionKey, unknown> = {
  schema: '',
  log: {},
  dns: { servers: [] },
  ntp: {},
  certificate: {},
  certificate_providers: [],
  http_clients: [],
  network_namespaces: [],
  endpoints: [],
  outbounds: [],
  route: {},
  experimental: {},
};

function asObject(value: unknown): JsonObject {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as JsonObject) : {};
}

function asObjectArray(value: unknown): JsonObject[] {
  return Array.isArray(value) ? value.map(asObject) : [];
}

function asString(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function asNumber(value: unknown, fallback = 0): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function asBoolean(value: unknown, fallback = false): boolean {
  return typeof value === 'boolean' ? value : fallback;
}

function asStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : [];
}

function prettyJson(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2);
}

function Field({
  label,
  children,
  span = 12,
  hint,
}: {
  label: string;
  children: ReactNode;
  span?: number;
  hint?: string;
}) {
  return (
    <Col xs={24} md={span}>
      <div className="singbox-field">
        <div className="singbox-field-label">{label}</div>
        {hint && <div className="singbox-field-hint">{hint}</div>}
        {children}
      </div>
    </Col>
  );
}

function TextField({
  value,
  onChange,
  placeholder,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <Input
      value={value}
      placeholder={placeholder}
      onChange={(event) => onChange(event.target.value)}
    />
  );
}

function NumberField({
  value,
  onChange,
  min = 0,
  max,
}: {
  value: number;
  onChange: (value: number) => void;
  min?: number;
  max?: number;
}) {
  return (
    <InputNumber
      style={{ width: '100%' }}
      value={value}
      min={min}
      max={max}
      onChange={(value) => onChange(typeof value === 'number' ? value : 0)}
    />
  );
}

function ToggleField({
  checked,
  onChange,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
}) {
  return <Switch checked={checked} onChange={onChange} />;
}

function StringListField({
  value,
  onChange,
  placeholder,
}: {
  value: string[];
  onChange: (value: string[]) => void;
  placeholder?: string;
}) {
  return (
    <Input
      value={value.join(', ')}
      placeholder={placeholder}
      onChange={(event) =>
        onChange(
          event.target.value
            .split(',')
            .map((item) => item.trim())
            .filter(Boolean),
        )
      }
    />
  );
}

function JsonModal({
  title,
  value,
  onApply,
  buttonText = 'JSON',
}: {
  title: string;
  value: unknown;
  onApply: (value: unknown) => void;
  buttonText?: string;
}) {
  const [open, setOpen] = useState(false);
  const [text, setText] = useState('');
  const [error, setError] = useState('');

  const openEditor = () => {
    setText(prettyJson(value));
    setError('');
    setOpen(true);
  };

  const apply = () => {
    try {
      const parsed: unknown = JSON.parse(text);
      if (
        Array.isArray(value)
          ? !Array.isArray(parsed)
          : !parsed || typeof parsed !== 'object' || Array.isArray(parsed)
      )
        throw new Error(Array.isArray(value) ? 'Ожидается JSON-массив.' : 'Ожидается JSON-объект.');
      onApply(parsed);
      setOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <Button icon={<CodeOutlined />} onClick={openEditor}>
        {buttonText}
      </Button>
      <Modal
        open={open}
        title={title}
        width={960}
        okText="Применить"
        cancelText="Отмена"
        onCancel={() => setOpen(false)}
        onOk={apply}
      >
        {error && <Alert type="error" showIcon message={error} style={{ marginBottom: 12 }} />}
        <Input.TextArea
          value={text}
          onChange={(event) => setText(event.target.value)}
          spellCheck={false}
          autoSize={{ minRows: 18, maxRows: 32 }}
          style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' }}
        />
      </Modal>
    </>
  );
}

function SectionHeader({
  title,
  description,
  onJson,
  onReset,
}: {
  title: string;
  description?: string;
  onJson?: () => void;
  onReset?: () => void;
}) {
  return (
    <div className="singbox-section-header">
      <div>
        <div className="singbox-section-title">{title}</div>
        {description && <div className="singbox-section-description">{description}</div>}
      </div>
      <Space wrap>
        {onReset && (
          <Button icon={<ReloadOutlined />} onClick={onReset}>
            Сбросить раздел
          </Button>
        )}
        {onJson && (
          <Button icon={<CodeOutlined />} onClick={onJson}>
            Расширенный JSON
          </Button>
        )}
      </Space>
    </div>
  );
}

function ListToolbar({ label, onAdd }: { label: string; onAdd: () => void }) {
  return (
    <div className="singbox-list-toolbar">
      <strong>{label}</strong>
      <Button type="primary" icon={<PlusOutlined />} onClick={onAdd}>
        Добавить
      </Button>
    </div>
  );
}

function CommonListItem({
  item,
  index,
  fields,
  onChange,
  onDelete,
  title,
  children,
}: {
  item: JsonObject;
  index: number;
  fields: Array<{ key: string; label: string; type?: 'number' | 'text' }>;
  onChange: (index: number, patch: JsonObject) => void;
  onDelete: (index: number) => void;
  title: string;
  children?: ReactNode;
}) {
  return (
    <Card size="small" className="singbox-item-card">
      <div className="singbox-item-card-header">
        <strong>{title}</strong>
        <Button danger type="text" icon={<DeleteOutlined />} onClick={() => onDelete(index)} />
      </div>
      <Row gutter={[12, 12]}>
        {fields.map((field) => (
          <Field key={field.key} label={field.label}>
            {field.type === 'number' ? (
              <NumberField
                value={asNumber(item[field.key])}
                onChange={(value) => onChange(index, { [field.key]: value })}
                min={0}
                max={65535}
              />
            ) : (
              <TextField
                value={asString(item[field.key])}
                onChange={(value) => onChange(index, { [field.key]: value })}
              />
            )}
          </Field>
        ))}
      </Row>
      {children && <div className="singbox-item-actions">{children}</div>}
    </Card>
  );
}

function singBoxHealthIssues(config: ConfigMap) {
  const issues: string[] = [];
  const outbounds = asObjectArray(config.outbounds);
  const tags = [...outbounds, ...asObjectArray(config.endpoints)]
    .map((item) => asString(item.tag))
    .filter(Boolean);
  const seen = new Set<string>();

  tags.forEach((tag) => {
    if (seen.has(tag)) issues.push('Дублирующийся outbound tag: ' + tag);
    seen.add(tag);
  });

  const tagSet = new Set(tags);
  outbounds.forEach((item) => {
    const type = asString(item.type);
    const name = asString(item.tag) || 'без tag';
    if (
      ['vless', 'vmess', 'trojan', 'shadowsocks', 'http', 'socks', 'hysteria2', 'tuic'].includes(
        type,
      )
    ) {
      if (!asString(item.server)) issues.push(type + ' "' + name + '": не указан сервер');
      if (!asNumber(item.server_port)) issues.push(type + ' "' + name + '": не указан порт');
    }
    if (['vless', 'vmess'].includes(type) && !asString(item.uuid)) {
      issues.push(type + ' "' + name + '": не указан UUID');
    }
    if (['selector', 'urltest'].includes(type)) {
      asStringArray(item.outbounds).forEach((target) => {
        if (!tagSet.has(target))
          issues.push(type + ' "' + name + '": отсутствует outbound "' + target + '"');
      });
    }
  });

  const route = asObject(config.route);
  if (asString(route.final) && !tagSet.has(asString(route.final))) {
    issues.push('route.final ссылается на отсутствующий outbound "' + asString(route.final) + '"');
  }

  const rules = Array.isArray(route.rules) ? route.rules : [];
  rules.forEach((raw, index) => {
    const rule = asObject(raw);
    if (asString(rule.outbound) && !tagSet.has(asString(rule.outbound))) {
      issues.push(
        'Route rule #' +
          (index + 1) +
          ' ссылается на отсутствующий outbound "' +
          asString(rule.outbound) +
          '"',
      );
    }
  });

  return Array.from(new Set(issues));
}

function SingBoxOutboundEditor({
  value,
  existingTags,
  onChange,
}: {
  value: JsonObject;
  existingTags: string[];
  onChange: (value: JsonObject) => void;
}) {
  const type = asString(value.type) || 'direct';
  const tls = asObject(value.tls);
  const transport = asObject(value.transport);
  const reality = asObject(tls.reality);

  const patch = (key: string, nextValue: unknown) => {
    const next = { ...value };
    if (nextValue === '' || nextValue === undefined || nextValue === null) delete next[key];
    else next[key] = nextValue;
    onChange(next);
  };

  const nested = (section: string, key: string, nextValue: unknown) => {
    const current = asObject(value[section]);
    const nextChild = { ...current };
    if (nextValue === '' || nextValue === undefined || nextValue === null) delete nextChild[key];
    else nextChild[key] = nextValue;
    const next = { ...value };
    if (Object.keys(nextChild).length) next[section] = nextChild;
    else delete next[section];
    onChange(next);
  };

  const tag = asString(value.tag);
  const duplicated = !!tag && existingTags.filter((item) => item === tag).length > (value ? 1 : 0);

  return (
    <>
      {duplicated && (
        <Alert
          type="warning"
          showIcon
          message="Tag уже используется. Укажите уникальное имя."
          style={{ marginBottom: 12 }}
        />
      )}
      <Tabs
        items={[
          {
            key: 'basic',
            label: 'Основное',
            children: (
              <Card size="small">
                <Row gutter={[12, 14]}>
                  <Field label="Протокол">
                    <Select
                      value={type}
                      style={{ width: '100%' }}
                      options={[
                        'direct',
                        'block',
                        'http',
                        'socks',
                        'shadowsocks',
                        'vmess',
                        'vless',
                        'trojan',
                        'hysteria2',
                        'tuic',
                        'selector',
                        'urltest',
                        'shadowtls',
                        'ssh',
                      ].map((v) => ({ value: v, label: v }))}
                      onChange={(next) =>
                        onChange(
                          outboundDefaults(
                            next,
                            asString(value.tag) || next,
                            asStringArray(value.outbounds),
                          ),
                        )
                      }
                    />
                  </Field>
                  <Field
                    label="Название подключения"
                    hint="Уникальное имя, по которому подключение выбирается в правилах."
                  >
                    <TextField
                      value={asString(value.tag)}
                      onChange={(v) => patch('tag', v)}
                      placeholder="proxy-1"
                    />
                  </Field>

                  {['selector', 'urltest'].includes(type) ? (
                    <>
                      <Field label="Outbounds группы" span={24}>
                        <Select
                          mode="multiple"
                          style={{ width: '100%' }}
                          value={asStringArray(value.outbounds)}
                          options={existingTags
                            .filter((tag) => tag !== asString(value.tag))
                            .map((tag) => ({ value: tag, label: tag }))}
                          onChange={(v) => patch('outbounds', v)}
                          placeholder="Выберите исходящие"
                        />
                      </Field>
                      {type === 'urltest' && (
                        <>
                          <Field label="URL проверки">
                            <TextField
                              value={asString(value.url)}
                              onChange={(v) => patch('url', v)}
                              placeholder="https://www.gstatic.com/generate_204"
                            />
                          </Field>
                          <Field label="Интервал">
                            <TextField
                              value={asString(value.interval)}
                              onChange={(v) => patch('interval', v)}
                              placeholder="3m"
                            />
                          </Field>
                          <Field label="Допуск">
                            <NumberField
                              value={asNumber(value.tolerance)}
                              onChange={(v) => patch('tolerance', v)}
                            />
                          </Field>
                        </>
                      )}
                    </>
                  ) : (
                    <>
                      {[
                        'http',
                        'socks',
                        'shadowsocks',
                        'vmess',
                        'vless',
                        'trojan',
                        'hysteria2',
                        'tuic',
                        'shadowtls',
                        'ssh',
                      ].includes(type) && (
                        <>
                          <Field label="Сервер">
                            <TextField
                              value={asString(value.server)}
                              onChange={(v) => patch('server', v)}
                              placeholder="example.com"
                            />
                          </Field>
                          <Field label="Порт">
                            <NumberField
                              value={asNumber(value.server_port)}
                              onChange={(v) => patch('server_port', v)}
                              min={1}
                              max={65535}
                            />
                          </Field>
                        </>
                      )}
                      {['vless', 'vmess', 'tuic'].includes(type) && (
                        <Field label="UUID">
                          <TextField
                            value={asString(value.uuid)}
                            onChange={(v) => patch('uuid', v)}
                          />
                        </Field>
                      )}
                      {type === 'vless' && (
                        <Field label="Flow">
                          <TextField
                            value={asString(value.flow)}
                            onChange={(v) => patch('flow', v)}
                            placeholder="xtls-rprx-vision"
                          />
                        </Field>
                      )}
                      {['trojan', 'hysteria2', 'tuic'].includes(type) && (
                        <Field label="Пароль">
                          <Input.Password
                            value={asString(value.password)}
                            onChange={(e) => patch('password', e.target.value)}
                          />
                        </Field>
                      )}
                      {['http', 'socks'].includes(type) && (
                        <>
                          <Field label="Логин">
                            <TextField
                              value={asString(value.username)}
                              onChange={(v) => patch('username', v)}
                            />
                          </Field>
                          <Field label="Пароль">
                            <Input.Password
                              value={asString(value.password)}
                              onChange={(e) => patch('password', e.target.value)}
                            />
                          </Field>
                        </>
                      )}
                      {type === 'shadowsocks' && (
                        <>
                          <Field label="Метод">
                            <TextField
                              value={asString(value.method)}
                              onChange={(v) => patch('method', v)}
                              placeholder="2022-blake3-aes-128-gcm"
                            />
                          </Field>
                          <Field label="Пароль">
                            <Input.Password
                              value={asString(value.password)}
                              onChange={(e) => patch('password', e.target.value)}
                            />
                          </Field>
                        </>
                      )}
                      {type === 'ssh' && (
                        <Field label="Пользователь">
                          <TextField
                            value={asString(value.user)}
                            onChange={(v) => patch('user', v)}
                            placeholder="root"
                          />
                        </Field>
                      )}
                      <Field
                        label="Подключиться через другой выход"
                        hint="Оставьте пустым для обычного подключения."
                      >
                        <TextField
                          value={asString(value.detour)}
                          onChange={(v) => patch('detour', v)}
                        />
                      </Field>
                    </>
                  )}
                </Row>
              </Card>
            ),
          },
          {
            key: 'tls',
            label: 'TLS / Reality',
            children: (
              <Card size="small">
                <Row gutter={[12, 14]}>
                  <Field label="TLS">
                    <Switch
                      checked={!!value.tls}
                      onChange={(checked) => {
                        if (checked) onChange({ ...value, tls: { enabled: true } });
                        else
                          onChange(
                            (() => {
                              const next = { ...value };
                              delete next.tls;
                              return next;
                            })(),
                          );
                      }}
                    />
                  </Field>
                  <Field label="SNI">
                    <TextField
                      value={asString(tls.server_name)}
                      onChange={(v) => nested('tls', 'server_name', v)}
                      placeholder="example.com"
                    />
                  </Field>
                  <Field label="Insecure">
                    <Switch
                      checked={!!tls.insecure}
                      disabled={!value.tls}
                      onChange={(v) => nested('tls', 'insecure', v)}
                    />
                  </Field>
                  <Field label="ALPN">
                    <Select
                      mode="tags"
                      style={{ width: '100%' }}
                      disabled={!value.tls}
                      value={asStringArray(tls.alpn)}
                      onChange={(v) => nested('tls', 'alpn', v)}
                    />
                  </Field>
                  <Field label="Reality">
                    <Switch
                      checked={!!reality.enabled}
                      disabled={!value.tls}
                      onChange={(v) => nested('tls', 'reality', { ...reality, enabled: v })}
                    />
                  </Field>
                  <Field label="Public key">
                    <TextField
                      value={asString(reality.public_key)}
                      onChange={(v) => nested('tls', 'reality', { ...reality, public_key: v })}
                    />
                  </Field>
                  <Field label="Short ID">
                    <TextField
                      value={asString(reality.short_id)}
                      onChange={(v) => nested('tls', 'reality', { ...reality, short_id: v })}
                    />
                  </Field>
                  <Field label="Certificate path">
                    <TextField
                      value={asString(tls.certificate_path)}
                      onChange={(v) => nested('tls', 'certificate_path', v)}
                    />
                  </Field>
                </Row>
              </Card>
            ),
          },
          {
            key: 'transport',
            label: 'Транспорт',
            children: (
              <Card size="small">
                <Row gutter={[12, 14]}>
                  <Field label="Transport">
                    <Select
                      allowClear
                      style={{ width: '100%' }}
                      value={asString(transport.type) || undefined}
                      options={['http', 'ws', 'grpc', 'httpupgrade', 'quic'].map((v) => ({
                        value: v,
                        label: v,
                      }))}
                      onChange={(v) => {
                        if (!v)
                          onChange(
                            (() => {
                              const next = { ...value };
                              delete next.transport;
                              return next;
                            })(),
                          );
                        else onChange({ ...value, transport: { ...transport, type: v } });
                      }}
                    />
                  </Field>
                  {['http', 'ws', 'httpupgrade'].includes(asString(transport.type)) && (
                    <Field label="Path">
                      <TextField
                        value={asString(transport.path)}
                        onChange={(v) => nested('transport', 'path', v)}
                        placeholder="/"
                      />
                    </Field>
                  )}
                  {['http', 'ws'].includes(asString(transport.type)) && (
                    <Field label="Host">
                      <TextField
                        value={asString(asObject(transport.headers).Host)}
                        onChange={(v) =>
                          nested('transport', 'headers', {
                            ...asObject(transport.headers),
                            Host: v,
                          })
                        }
                      />
                    </Field>
                  )}
                  {['grpc', 'quic'].includes(asString(transport.type)) && (
                    <Field label="Service name">
                      <TextField
                        value={asString(transport.service_name)}
                        onChange={(v) => nested('transport', 'service_name', v)}
                      />
                    </Field>
                  )}
                  <Field label="Headers JSON" span={24}>
                    <Input.TextArea
                      autoSize={{ minRows: 3, maxRows: 8 }}
                      value={JSON.stringify(asObject(transport.headers), null, 2)}
                      onChange={(e) => {
                        try {
                          nested('transport', 'headers', JSON.parse(e.target.value || '{}'));
                        } catch {
                          /* wait for valid json */
                        }
                      }}
                    />
                  </Field>
                </Row>
              </Card>
            ),
          },
          {
            key: 'advanced',
            label: 'JSON',
            children: (
              <Card size="small">
                <Alert
                  type="info"
                  showIcon
                  message="Расширенный режим"
                  description="Любой параметр sing-box можно задать вручную, не теряя остальную форму."
                  style={{ marginBottom: 12 }}
                />
                <JsonModal
                  title="Исходящее подключение — JSON"
                  value={value}
                  onApply={(next) => onChange(asObject(next))}
                  buttonText="Редактировать JSON"
                />
              </Card>
            ),
          },
        ]}
      />
    </>
  );
}

function SingBoxOutboundModal({
  open,
  value,
  existingTags,
  onCancel,
  onSave,
}: {
  open: boolean;
  value: JsonObject | null;
  existingTags: string[];
  onCancel: () => void;
  onSave: (value: JsonObject) => void;
}) {
  const existingTagsKey = JSON.stringify(existingTags);
  const [error, setError] = useState('');
  const [draft, setDraft] = useState<JsonObject>({});
  useEffect(() => {
    if (!open) return;
    const nextDraft = value
      ? JSON.parse(JSON.stringify(value))
      : { type: 'direct', tag: uniqueTag('direct', JSON.parse(existingTagsKey) as string[]) };
    const timer = window.setTimeout(() => {
      setDraft(nextDraft);
      setError('');
    }, 0);
    return () => window.clearTimeout(timer);
  }, [open, value, existingTagsKey]);

  return (
    <Modal
      open={open}
      width={980}
      title={value ? 'Изменение outbound' : 'Новый outbound'}
      okText="Сохранить"
      cancelText="Отмена"
      onCancel={onCancel}
      onOk={() => {
        const errors = outboundErrors(
          draft,
          existingTags.filter(
            (_tag, index) => index !== (value ? existingTags.indexOf(asString(value.tag)) : -1),
          ),
        );
        if (errors.length) {
          setError(errors.join(' '));
          return;
        }
        onSave({ ...draft, tag: asString(draft.tag).trim() });
      }}
      destroyOnHidden
    >
      {error && <Alert type="error" showIcon title={error} style={{ marginBottom: 12 }} />}
      <SingBoxOutboundEditor value={draft} existingTags={existingTags} onChange={setDraft} />
    </Modal>
  );
}

function SingBoxRouteRuleModal({
  open,
  value,
  inboundTags,
  outboundTags,
  ruleSetTags,
  onCancel,
  onSave,
}: {
  open: boolean;
  value: JsonObject | null;
  inboundTags: string[];
  outboundTags: string[];
  ruleSetTags: string[];
  onCancel: () => void;
  onSave: (value: JsonObject) => void;
}) {
  const [draft, setDraft] = useState<JsonObject>({});
  useEffect(() => {
    if (!open) return;
    const nextDraft = value ? JSON.parse(JSON.stringify(value)) : {};
    const timer = window.setTimeout(() => setDraft(nextDraft), 0);
    return () => window.clearTimeout(timer);
  }, [open, value]);

  const patch = (key: string, nextValue: unknown) => {
    setDraft((current) => {
      const next = { ...current };
      if (
        nextValue === '' ||
        nextValue === undefined ||
        (Array.isArray(nextValue) && nextValue.length === 0)
      )
        delete next[key];
      else next[key] = nextValue;
      return next;
    });
  };

  return (
    <Modal
      open={open}
      width={920}
      title={value ? 'Изменение правила' : 'Новое правило'}
      okText="Сохранить"
      cancelText="Отмена"
      onCancel={onCancel}
      onOk={() => onSave(draft)}
    >
      <Alert
        type="info"
        showIcon
        message="Фильтр → назначение"
        description="Правила редактируются так же, как в Xray: условия собираются полями, JSON оставлен для редких параметров."
        style={{ marginBottom: 12 }}
      />
      <Tabs
        items={[
          {
            key: 'match',
            label: 'Условия',
            children: (
              <Card size="small">
                <Row gutter={[12, 14]}>
                  <Field label="Домены">
                    <Select
                      mode="tags"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.domain)}
                      onChange={(v) => patch('domain', v)}
                      placeholder="example.com"
                    />
                  </Field>
                  <Field label="Domain suffix">
                    <Select
                      mode="tags"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.domain_suffix)}
                      onChange={(v) => patch('domain_suffix', v)}
                    />
                  </Field>
                  <Field label="Domain keyword">
                    <Select
                      mode="tags"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.domain_keyword)}
                      onChange={(v) => patch('domain_keyword', v)}
                    />
                  </Field>
                  <Field label="IP / CIDR">
                    <Select
                      mode="tags"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.ip_cidr)}
                      onChange={(v) => patch('ip_cidr', v)}
                    />
                  </Field>
                  <Field label="Source IP / CIDR">
                    <Select
                      mode="tags"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.source_ip_cidr)}
                      onChange={(v) => patch('source_ip_cidr', v)}
                    />
                  </Field>
                  <Field label="Ports">
                    <Input
                      value={
                        Array.isArray(draft.port) ? draft.port.join(', ') : asString(draft.port)
                      }
                      onChange={(e) => patch('port', e.target.value)}
                      placeholder="80, 443, 1000:2000"
                    />
                  </Field>
                  <Field label="Source ports">
                    <Input
                      value={
                        Array.isArray(draft.source_port)
                          ? draft.source_port.join(', ')
                          : asString(draft.source_port)
                      }
                      onChange={(e) => patch('source_port', e.target.value)}
                    />
                  </Field>
                  <Field label="Protocol">
                    <Select
                      mode="multiple"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.protocol)}
                      options={['http', 'tls', 'quic', 'dns', 'bittorrent'].map((v) => ({
                        value: v,
                        label: v,
                      }))}
                      onChange={(v) => patch('protocol', v)}
                    />
                  </Field>
                  <Field label="Network">
                    <Select
                      mode="multiple"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.network)}
                      options={['tcp', 'udp'].map((v) => ({ value: v, label: v }))}
                      onChange={(v) => patch('network', v)}
                    />
                  </Field>
                  <Field label="Inbound">
                    <Select
                      mode="multiple"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.inbound)}
                      options={inboundTags.map((v) => ({ value: v, label: v }))}
                      onChange={(v) => patch('inbound', v)}
                    />
                  </Field>
                  <Field label="Rule-set">
                    <Select
                      mode="multiple"
                      style={{ width: '100%' }}
                      value={asStringArray(draft.rule_set)}
                      options={ruleSetTags.map((v) => ({ value: v, label: v }))}
                      onChange={(v) => patch('rule_set', v)}
                    />
                  </Field>
                </Row>
              </Card>
            ),
          },
          {
            key: 'target',
            label: 'Назначение',
            children: (
              <Card size="small">
                <Row gutter={[12, 14]}>
                  <Field label="Outbound">
                    <Select
                      allowClear
                      style={{ width: '100%' }}
                      value={asString(draft.outbound) || undefined}
                      options={outboundTags.map((v) => ({ value: v, label: v }))}
                      onChange={(v) => patch('outbound', v)}
                    />
                  </Field>
                  <Field label="Action">
                    <Select
                      allowClear
                      style={{ width: '100%' }}
                      value={asString(draft.action) || undefined}
                      options={[
                        'route',
                        'sniff',
                        'resolve',
                        'hijack-dns',
                        'reject',
                        'selector',
                      ].map((v) => ({ value: v, label: v }))}
                      onChange={(v) => patch('action', v)}
                    />
                  </Field>
                  <Field label="Комментарий" span={24}>
                    <Input
                      value={asString(draft.comment)}
                      onChange={(e) => patch('comment', e.target.value)}
                      placeholder="Telegram через proxy"
                    />
                  </Field>
                </Row>
              </Card>
            ),
          },
          {
            key: 'advanced',
            label: 'JSON',
            children: (
              <Card size="small">
                <JsonModal
                  title="Правило — JSON"
                  value={draft}
                  onApply={(next) => setDraft(asObject(next))}
                  buttonText="Редактировать JSON"
                />
              </Card>
            ),
          },
        ]}
      />
    </Modal>
  );
}

export default function SingBoxPage() {
  const { t } = useTranslation();
  const [messageApi, contextHolder] = message.useMessage();
  const [modalApi, modalContextHolder] = Modal.useModal();
  const { antdThemeConfig, isDark, isUltra } = useTheme();
  const location = useLocation();
  const searchParams = new URLSearchParams(location.search);
  const navigate = useNavigate();
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [config, setConfig] = useState<ConfigMap>({});
  const sectionSlug = location.hash.replace(/^#/, '');
  const sectionKeys = useMemo(
    () =>
      new Set([
        'basic',
        'dns',
        'routing',
        'outbound',
        'endpoints',
        'certificates',
        'network',
        'advanced',
      ]),
    [],
  );
  const activeSection = sectionKeys.has(sectionSlug) ? sectionSlug : 'basic';
  const [advancedMode, setAdvancedMode] = useState(false);
  const expertSection = ['advanced', 'endpoints', 'certificates', 'network'].includes(
    activeSection,
  );
  const showAdvanced = advancedMode || expertSection;
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [fetchError, setFetchError] = useState('');
  const SECTION_LABELS: Record<string, ReactNode> = {
    basic: 'Основные',
    dns: 'DNS',
    routing: 'Маршрутизация',
    outbound: 'Исходящие',
    endpoints: 'Endpoints',
    certificates: 'Сертификаты',
    network: 'Сеть',
    advanced: 'Расширенные',
  };

  const [outboundModalOpen, setOutboundModalOpen] = useState(false);
  const [editingOutbound, setEditingOutbound] = useState<number | null>(null);
  const [routeRuleModalOpen, setRouteRuleModalOpen] = useState(false);
  const [editingRouteRule, setEditingRouteRule] = useState<number | null>(null);
  const [shareLinkOpen, setShareLinkOpen] = useState(false);
  const [shareLink, setShareLink] = useState('');

  const sectionValue = useCallback((key: SectionKey, source: ConfigMap) => {
    const sourceKey = key === 'schema' ? '$schema' : key;
    return source[sourceKey] ?? sectionFallbacks[key];
  }, []);

  const refresh = useCallback(async () => {
    setLoading(true);
    setFetchError('');
    try {
      const msg = (await HttpUtil.get('/panel/api/setting/singbox/config')) as ApiMsg<Snapshot>;
      if (!msg?.success || !msg.obj?.config) {
        throw new Error(msg?.msg || 'Не удалось загрузить настройки sing-box');
      }
      setSnapshot(msg.obj);
      setConfig(msg.obj.config);
    } catch (error) {
      setFetchError(error instanceof Error ? error.message : String(error));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const initial = window.setTimeout(() => void refresh(), 0);
    return () => window.clearTimeout(initial);
  }, [refresh]);

  const updateSection = useCallback((key: SectionKey, value: unknown) => {
    setConfig((current) => ({
      ...current,
      [key === 'schema' ? '$schema' : key]: value,
    }));
  }, []);

  const patchSection = useCallback((key: SectionKey, patch: JsonObject) => {
    setConfig((current) => {
      const currentValue = asObject(current[key]);
      return {
        ...current,
        [key]: { ...currentValue, ...patch },
      };
    });
  }, []);

  const save = async () => {
    try {
      const next: ConfigMap = { ...config };
      delete next.inbounds;
      delete next.services;

      setSaving(true);
      const msg = (await HttpUtil.post('/panel/api/setting/singbox/config', {
        config: JSON.stringify(next),
      })) as ApiMsg;
      if (!msg?.success) throw new Error(msg?.msg || 'Не удалось сохранить настройки sing-box');
      messageApi.success(t('pages.singBox.saved'));
      await refresh();
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  const reset = async () => {
    setSaving(true);
    try {
      const msg = (await HttpUtil.post('/panel/api/setting/singbox/config/reset')) as ApiMsg;
      if (!msg?.success)
        throw new Error(msg?.msg || 'Не удалось вернуть сгенерированные настройки');
      messageApi.success(t('pages.singBox.resetDone'));
      await refresh();
    } catch (error) {
      messageApi.error(error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  const renderLog = () => {
    const value = asObject(sectionValue('log', config));
    return (
      <>
        <SectionHeader
          title="Логирование"
          description="Основные параметры журналирования sing-box."
          onReset={() => updateSection('log', sectionFallbacks.log)}
        />
        <Card>
          <Row gutter={[12, 16]}>
            <Field label="Логирование отключено">
              <ToggleField
                checked={asBoolean(value.disabled)}
                onChange={(next) => patchSection('log', { disabled: next })}
              />
            </Field>
            <Field label="Уровень">
              <Select
                style={{ width: '100%' }}
                value={asString(value.level) || 'info'}
                options={['trace', 'debug', 'info', 'warn', 'error', 'fatal', 'panic'].map((v) => ({
                  label: v,
                  value: v,
                }))}
                onChange={(next) => patchSection('log', { level: next })}
              />
            </Field>
            <Field label="Файл вывода" span={24}>
              <TextField
                value={asString(value.output)}
                placeholder="box.log"
                onChange={(next) => patchSection('log', { output: next })}
              />
            </Field>
            <Field label="Добавлять timestamp">
              <ToggleField
                checked={asBoolean(value.timestamp)}
                onChange={(next) => patchSection('log', { timestamp: next })}
              />
            </Field>
          </Row>
        </Card>
        <div className="singbox-json-action">
          <JsonModal
            title="Лог — расширенный JSON"
            value={value}
            onApply={(next) => updateSection('log', asObject(next))}
          />
        </div>
      </>
    );
  };

  const renderDns = () => {
    const value = asObject(sectionValue('dns', config));
    const servers = asObjectArray(value.servers);
    const rules = Array.isArray(value.rules) ? value.rules : [];
    const updateServer = (index: number, patch: JsonObject) => {
      const next = [...servers];
      next[index] = { ...next[index], ...patch };
      updateSection('dns', { ...value, servers: next });
    };

    return (
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        <Card>
          <SectionHeader
            title="DNS"
            description="Базовые параметры вынесены в понятные поля, отдельные DNS-серверы редактируются как записи."
          />
          <Row gutter={[12, 16]}>
            <Field label="Финальный DNS">
              <Select
                allowClear
                style={{ width: '100%' }}
                value={asString(value.final) || undefined}
                options={servers.map((s) => ({
                  value: asString(s.tag),
                  label: asString(s.tag) || asString(s.server) || 'DNS',
                }))}
                onChange={(v) => patchSection('dns', { final: v })}
              />
            </Field>
            <Field label="Стратегия">
              <Select
                allowClear
                style={{ width: '100%' }}
                value={asString(value.strategy) || undefined}
                options={['prefer_ipv4', 'prefer_ipv6', 'ipv4_only', 'ipv6_only'].map((v) => ({
                  value: v,
                  label: v,
                }))}
                onChange={(v) => patchSection('dns', { strategy: v })}
              />
            </Field>
            <Field label="Кэш">
              <Switch
                checked={!asBoolean(value.disable_cache)}
                onChange={(v) => setConfig(setDnsCache(config, v))}
              />
            </Field>
            <Field label="Оптимистический кэш">
              <Switch
                checked={optimisticCacheEnabled(value)}
                onChange={(v) => setConfig(setOptimisticCache(config, v))}
              />
            </Field>
            <Field label="Client subnet">
              <TextField
                value={asString(value.client_subnet)}
                onChange={(v) => patchSection('dns', { client_subnet: v })}
              />
            </Field>
            <Field label="Cache capacity">
              <NumberField
                value={asNumber(value.cache_capacity)}
                onChange={(v) => patchSection('dns', { cache_capacity: v })}
              />
            </Field>
          </Row>
        </Card>

        <Card>
          <div className="singbox-list-toolbar">
            <div>
              <div className="singbox-card-title">DNS-серверы</div>
              <div className="singbox-card-subtitle">
                Частые поля доступны прямо в таблице. Для TLS, HTTPS, fakeip и специальных
                параметров есть JSON записи.
              </div>
            </div>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() =>
                updateSection('dns', {
                  ...value,
                  servers: [...servers, { type: 'local', tag: 'dns-' + (servers.length + 1) }],
                })
              }
            >
              Добавить DNS
            </Button>
          </div>
          <Table
            size="small"
            pagination={false}
            rowKey={(_, index) => String(index)}
            dataSource={servers}
            locale={{ emptyText: <Empty description="DNS-серверы ещё не добавлены" /> }}
            columns={[
              {
                title: 'Tag',
                render: (_: unknown, row: JsonObject) => (
                  <Tag color="blue">{asString(row.tag) || 'без tag'}</Tag>
                ),
              },
              {
                title: 'Тип',
                render: (_: unknown, row: JsonObject, index: number) => (
                  <Select
                    size="small"
                    value={asString(row.type) || 'local'}
                    style={{ minWidth: 150 }}
                    options={[
                      'local',
                      'hosts',
                      'tcp',
                      'udp',
                      'tls',
                      'quic',
                      'https',
                      'h3',
                      'dhcp',
                      'mdns',
                      'fakeip',
                      'tailscale',
                      'openconnect',
                      'openvpn',
                      'resolved',
                    ].map((v) => ({ value: v, label: v }))}
                    onChange={(v) => updateServer(index, { type: v })}
                  />
                ),
              },
              {
                title: 'Server',
                render: (_: unknown, row: JsonObject, index: number) => (
                  <TextField
                    value={asString(row.server)}
                    onChange={(v) => updateServer(index, { server: v })}
                    placeholder="1.1.1.1 / https://..."
                  />
                ),
              },
              {
                title: 'Port',
                width: 90,
                render: (_: unknown, row: JsonObject, index: number) => (
                  <NumberField
                    value={asNumber(row.server_port)}
                    onChange={(v) => updateServer(index, { server_port: v })}
                    min={1}
                    max={65535}
                  />
                ),
              },
              {
                title: 'Detour',
                render: (_: unknown, row: JsonObject, index: number) => (
                  <TextField
                    value={asString(row.detour)}
                    onChange={(v) => updateServer(index, { detour: v })}
                  />
                ),
              },
              {
                title: '',
                width: 170,
                render: (_: unknown, row: JsonObject, index: number) => (
                  <Space>
                    <JsonModal
                      title={(asString(row.tag) || 'DNS') + ' — JSON'}
                      value={row}
                      onApply={(next) => updateServer(index, asObject(next))}
                    />
                    <Button
                      size="small"
                      danger
                      icon={<DeleteOutlined />}
                      onClick={() =>
                        updateSection('dns', {
                          ...value,
                          servers: servers.filter((_, i) => i !== index),
                        })
                      }
                    />
                  </Space>
                ),
              },
            ]}
          />
          <div className="singbox-inline-actions">
            <JsonModal
              title="DNS rules"
              value={rules}
              onApply={(next) => patchSection('dns', { rules: Array.isArray(next) ? next : [] })}
              buttonText="DNS rules"
            />
            <JsonModal
              title="DNS целиком"
              value={value}
              onApply={(next) => updateSection('dns', asObject(next))}
              buttonText="Весь DNS"
            />
          </div>
        </Card>
      </Space>
    );
  };

  const renderNtp = () => {
    const value = asObject(sectionValue('ntp', config));
    return (
      <>
        <SectionHeader title="NTP" description="Синхронизация времени для протоколов sing-box." />
        <Card>
          <Row gutter={[12, 16]}>
            <Field label="Включено">
              <ToggleField
                checked={asBoolean(value.enabled)}
                onChange={(next) => patchSection('ntp', { enabled: next })}
              />
            </Field>
            <Field label="NTP-сервер">
              <TextField
                value={asString(value.server)}
                onChange={(next) => patchSection('ntp', { server: next })}
              />
            </Field>
            <Field label="Порт">
              <NumberField
                value={asNumber(value.server_port, 123)}
                onChange={(next) => patchSection('ntp', { server_port: next })}
                max={65535}
              />
            </Field>
            <Field label="Интервал">
              <TextField
                value={asString(value.interval)}
                onChange={(next) => patchSection('ntp', { interval: next })}
                placeholder="30m"
              />
            </Field>
            <Field label="Domain resolver">
              <TextField
                value={asString(value.domain_resolver)}
                onChange={(next) => patchSection('ntp', { domain_resolver: next })}
              />
            </Field>
            <Field label="Detour">
              <TextField
                value={asString(value.detour)}
                onChange={(next) => patchSection('ntp', { detour: next })}
              />
            </Field>
          </Row>
        </Card>
        <div className="singbox-json-action">
          <JsonModal
            title="NTP — расширенный JSON"
            value={value}
            onApply={(next) => updateSection('ntp', asObject(next))}
          />
        </div>
      </>
    );
  };

  const renderCertificate = () => {
    const value = asObject(sectionValue('certificate', config));
    return (
      <>
        <SectionHeader title="Сертификаты" description="Доверенные CA и пути к сертификатам." />
        <Card>
          <Row gutter={[12, 16]}>
            <Field label="Хранилище">
              <Select
                style={{ width: '100%' }}
                value={asString(value.store) || 'system'}
                options={['system', 'mozilla', 'chrome', 'none'].map((v) => ({
                  label: v,
                  value: v,
                }))}
                onChange={(next) => patchSection('certificate', { store: next })}
              />
            </Field>
            <Field label="Каталог сертификатов" span={24}>
              <StringListField
                value={asStringArray(value.certificate_directory_path)}
                onChange={(next) =>
                  patchSection('certificate', { certificate_directory_path: next })
                }
              />
            </Field>
            <Field label="Пути сертификатов" span={24}>
              <StringListField
                value={asStringArray(value.certificate_path)}
                onChange={(next) => patchSection('certificate', { certificate_path: next })}
              />
            </Field>
          </Row>
        </Card>
        <div className="singbox-json-action">
          <JsonModal
            title="Сертификаты — расширенный JSON"
            value={value}
            onApply={(next) => updateSection('certificate', asObject(next))}
          />
        </div>
      </>
    );
  };

  const renderCertificateProviders = () => {
    const items = asObjectArray(sectionValue('certificate_providers', config));
    return (
      <>
        <SectionHeader
          title="Провайдеры сертификатов"
          description="ACME, Tailscale и Cloudflare Origin CA."
        />
        <Card className="singbox-list-card">
          <ListToolbar
            label="Провайдеры"
            onAdd={() =>
              updateSection('certificate_providers', [
                ...items,
                { type: 'acme', tag: `cert-${items.length + 1}`, domain: [] },
              ])
            }
          />
          <Space orientation="vertical" style={{ width: '100%' }} size={12}>
            {items.map((item, index) => (
              <Card size="small" className="singbox-item-card" key={`cert-provider-${index}`}>
                <div className="singbox-item-card-header">
                  <strong>{asString(item.tag) || `Провайдер #${index + 1}`}</strong>
                  <Button
                    danger
                    type="text"
                    icon={<DeleteOutlined />}
                    onClick={() =>
                      updateSection(
                        'certificate_providers',
                        items.filter((_, i) => i !== index),
                      )
                    }
                  />
                </div>
                <Row gutter={[12, 12]}>
                  <Field label="Тип">
                    <Select
                      style={{ width: '100%' }}
                      value={asString(item.type) || 'acme'}
                      options={['acme', 'tailscale', 'cloudflare-origin-ca'].map((v) => ({
                        label: v,
                        value: v,
                      }))}
                      onChange={(next) => {
                        const nextItems = [...items];
                        nextItems[index] = { ...item, type: next };
                        updateSection('certificate_providers', nextItems);
                      }}
                    />
                  </Field>
                  <Field label="Tag">
                    <TextField
                      value={asString(item.tag)}
                      onChange={(next) => {
                        const nextItems = [...items];
                        nextItems[index] = { ...item, tag: next };
                        updateSection('certificate_providers', nextItems);
                      }}
                    />
                  </Field>
                  <Field label="Домены">
                    <StringListField
                      value={asStringArray(item.domain)}
                      onChange={(next) => {
                        const nextItems = [...items];
                        nextItems[index] = { ...item, domain: next };
                        updateSection('certificate_providers', nextItems);
                      }}
                    />
                  </Field>
                  <Field label="Email">
                    <TextField
                      value={asString(item.email)}
                      onChange={(next) => {
                        const nextItems = [...items];
                        nextItems[index] = { ...item, email: next };
                        updateSection('certificate_providers', nextItems);
                      }}
                    />
                  </Field>
                  <Field label="Data directory">
                    <TextField
                      value={asString(item.data_directory)}
                      onChange={(next) => {
                        const nextItems = [...items];
                        nextItems[index] = { ...item, data_directory: next };
                        updateSection('certificate_providers', nextItems);
                      }}
                    />
                  </Field>
                  <Field label="Provider">
                    <TextField
                      value={asString(item.provider)}
                      onChange={(next) => {
                        const nextItems = [...items];
                        nextItems[index] = { ...item, provider: next };
                        updateSection('certificate_providers', nextItems);
                      }}
                    />
                  </Field>
                </Row>
                <div className="singbox-item-actions">
                  <JsonModal
                    title={`${asString(item.tag) || 'Провайдер'} — JSON`}
                    value={item}
                    onApply={(next) => {
                      const nextItems = [...items];
                      nextItems[index] = asObject(next);
                      updateSection('certificate_providers', nextItems);
                    }}
                  />
                </div>
              </Card>
            ))}
          </Space>
        </Card>
      </>
    );
  };

  const renderHttpClients = () => {
    const items = asObjectArray(sectionValue('http_clients', config));
    const updateItem = (index: number, patch: JsonObject) => {
      const next = [...items];
      next[index] = { ...next[index], ...patch };
      updateSection('http_clients', next);
    };
    return (
      <>
        <SectionHeader
          title="HTTP-клиенты"
          description="Общие HTTP-клиенты для удалённых rule-set и служебных запросов."
        />
        <Card className="singbox-list-card">
          <ListToolbar
            label="HTTP-клиенты"
            onAdd={() =>
              updateSection('http_clients', [
                ...items,
                { tag: `http-${items.length + 1}`, engine: 'go', version: 2 },
              ])
            }
          />
          <Space orientation="vertical" style={{ width: '100%' }} size={12}>
            {items.map((item, index) => {
              const tls = asObject(item.tls);
              return (
                <Card size="small" className="singbox-item-card" key={`http-client-${index}`}>
                  <div className="singbox-item-card-header">
                    <strong>{asString(item.tag) || `HTTP-клиент #${index + 1}`}</strong>
                    <Button
                      danger
                      type="text"
                      icon={<DeleteOutlined />}
                      onClick={() =>
                        updateSection(
                          'http_clients',
                          items.filter((_, i) => i !== index),
                        )
                      }
                    />
                  </div>
                  <Row gutter={[12, 12]}>
                    <Field label="Tag">
                      <TextField
                        value={asString(item.tag)}
                        onChange={(next) => updateItem(index, { tag: next })}
                      />
                    </Field>
                    <Field label="Engine">
                      <Select
                        style={{ width: '100%' }}
                        value={asString(item.engine) || 'go'}
                        options={['go', 'apple'].map((v) => ({ label: v, value: v }))}
                        onChange={(next) => updateItem(index, { engine: next })}
                      />
                    </Field>
                    <Field label="HTTP version">
                      <NumberField
                        value={asNumber(item.version, 2)}
                        min={1}
                        max={3}
                        onChange={(next) => updateItem(index, { version: next })}
                      />
                    </Field>
                    <Field label="Disable fallback">
                      <ToggleField
                        checked={asBoolean(item.disable_version_fallback)}
                        onChange={(next) => updateItem(index, { disable_version_fallback: next })}
                      />
                    </Field>
                    <Field label="TLS server name">
                      <TextField
                        value={asString(tls.server_name)}
                        onChange={(next) =>
                          updateItem(index, { tls: { ...tls, server_name: next } })
                        }
                      />
                    </Field>
                    <Field label="TLS insecure">
                      <ToggleField
                        checked={asBoolean(tls.insecure)}
                        onChange={(next) => updateItem(index, { tls: { ...tls, insecure: next } })}
                      />
                    </Field>
                    <Field label="Headers">
                      <JsonModal
                        title={`${asString(item.tag) || 'HTTP-клиент'} — Headers`}
                        value={item.headers || {}}
                        onApply={(next) => updateItem(index, { headers: asObject(next) })}
                      />
                    </Field>
                  </Row>
                  <div className="singbox-item-actions">
                    <JsonModal
                      title={`${asString(item.tag) || 'HTTP-клиент'} — JSON`}
                      value={item}
                      onApply={(next) => updateItem(index, asObject(next))}
                    />
                  </div>
                </Card>
              );
            })}
          </Space>
        </Card>
      </>
    );
  };

  const renderNamespaces = () => {
    const items = asObjectArray(sectionValue('network_namespaces', config));
    return (
      <>
        <SectionHeader
          title="Сетевые пространства"
          description="Linux network namespaces для tun, listen и dial."
        />
        <Card className="singbox-list-card">
          <ListToolbar
            label="Namespaces"
            onAdd={() =>
              updateSection('network_namespaces', [
                ...items,
                { type: 'default', tag: `ns-${items.length + 1}` },
              ])
            }
          />
          <Space orientation="vertical" style={{ width: '100%' }} size={12}>
            {items.map((item, index) => (
              <CommonListItem
                key={`namespace-${index}`}
                item={item}
                index={index}
                title={asString(item.tag) || `Namespace #${index + 1}`}
                fields={[{ key: 'tag', label: 'Tag' }]}
                onChange={(i, patch) => {
                  const next = [...items];
                  next[i] = { ...next[i], ...patch };
                  updateSection('network_namespaces', next);
                }}
                onDelete={(i) =>
                  updateSection(
                    'network_namespaces',
                    items.filter((_, j) => j !== i),
                  )
                }
              >
                <Select
                  style={{ width: 180 }}
                  value={asString(item.type) || 'default'}
                  options={['default', 'unshare'].map((v) => ({ label: v, value: v }))}
                  onChange={(next) => {
                    const nextItems = [...items];
                    nextItems[index] = { ...nextItems[index], type: next };
                    updateSection('network_namespaces', nextItems);
                  }}
                />
              </CommonListItem>
            ))}
          </Space>
          {items.length > 0 && (
            <div className="singbox-inline-extra">
              <JsonModal
                title="Namespaces — JSON"
                value={items}
                onApply={(next) => updateSection('network_namespaces', asObjectArray(next))}
              />
            </div>
          )}
        </Card>
      </>
    );
  };

  const renderEndpoints = () => {
    const items = asObjectArray(sectionValue('endpoints', config));
    return (
      <>
        <SectionHeader
          title="Endpoints"
          description="WireGuard, Tailscale, OpenConnect и OpenVPN endpoints."
        />
        <Card className="singbox-list-card">
          <ListToolbar
            label="Endpoints"
            onAdd={() =>
              updateSection('endpoints', [
                ...items,
                { type: 'wireguard', tag: `endpoint-${items.length + 1}` },
              ])
            }
          />
          <Space orientation="vertical" style={{ width: '100%' }} size={12}>
            {items.map((item, index) => {
              const nextItems = [...items];
              const patch = (value: JsonObject) => {
                nextItems[index] = { ...nextItems[index], ...value };
                updateSection('endpoints', nextItems);
              };
              return (
                <Card size="small" className="singbox-item-card" key={`endpoint-${index}`}>
                  <div className="singbox-item-card-header">
                    <strong>{asString(item.tag) || `Endpoint #${index + 1}`}</strong>
                    <Button
                      danger
                      type="text"
                      icon={<DeleteOutlined />}
                      onClick={() =>
                        updateSection(
                          'endpoints',
                          items.filter((_, i) => i !== index),
                        )
                      }
                    />
                  </div>
                  <Row gutter={[12, 12]}>
                    <Field label="Тип">
                      <Select
                        style={{ width: '100%' }}
                        value={asString(item.type) || 'wireguard'}
                        options={[
                          'wireguard',
                          'tailscale',
                          'openconnect',
                          'openvpn-client',
                          'openvpn-server',
                        ].map((v) => ({ label: v, value: v }))}
                        onChange={(next) => patch({ type: next })}
                      />
                    </Field>
                    <Field label="Tag">
                      <TextField
                        value={asString(item.tag)}
                        onChange={(next) => patch({ tag: next })}
                      />
                    </Field>
                    <Field label="Server">
                      <TextField
                        value={asString(item.server)}
                        onChange={(next) => patch({ server: next })}
                      />
                    </Field>
                    <Field label="Port">
                      <NumberField
                        value={asNumber(item.server_port)}
                        max={65535}
                        onChange={(next) => patch({ server_port: next })}
                      />
                    </Field>
                    <Field label="Domain resolver">
                      <TextField
                        value={asString(item.domain_resolver)}
                        onChange={(next) => patch({ domain_resolver: next })}
                      />
                    </Field>
                    <Field label="Detour">
                      <TextField
                        value={asString(item.detour)}
                        onChange={(next) => patch({ detour: next })}
                      />
                    </Field>
                  </Row>
                  <div className="singbox-item-actions">
                    <JsonModal
                      title={`${asString(item.tag) || 'Endpoint'} — JSON`}
                      value={item}
                      onApply={(next) => patch(asObject(next))}
                    />
                  </div>
                </Card>
              );
            })}
          </Space>
        </Card>
      </>
    );
  };

  const renderOutbounds = () => {
    const items = asObjectArray(sectionValue('outbounds', config));

    return (
      <Card>
        <div className="singbox-section-header">
          <div>
            <div className="singbox-section-title">Исходящие</div>
            <div className="singbox-section-description">
              Теперь это список как в Xray: отдельный мастер для протокола, серверных параметров,
              TLS/Reality и транспорта.
            </div>
          </div>
          <Space wrap>
            <Button onClick={() => setShareLinkOpen(true)}>Импорт ссылки</Button>
            <Button
              icon={<PlusOutlined />}
              type="primary"
              onClick={() => {
                setEditingOutbound(null);
                setOutboundModalOpen(true);
              }}
            >
              Добавить outbound
            </Button>
          </Space>
        </div>
        <Table
          size="small"
          pagination={false}
          rowKey={(_, index) => String(index)}
          dataSource={items}
          locale={{
            emptyText: (
              <Empty description="Нет исходящих. Начните с direct, VLESS, VMess или Trojan." />
            ),
          }}
          columns={[
            {
              title: 'Tag',
              width: 200,
              render: (_: unknown, row: JsonObject) => (
                <Tag color="blue">{asString(row.tag) || 'без tag'}</Tag>
              ),
            },
            {
              title: 'Протокол',
              width: 130,
              render: (_: unknown, row: JsonObject) => asString(row.type) || '—',
            },
            {
              title: 'Endpoint',
              render: (_: unknown, row: JsonObject) => {
                const type = asString(row.type);
                if (['selector', 'urltest'].includes(type))
                  return asStringArray(row.outbounds).length + ' outbound в группе';
                if (row.server)
                  return asString(row.server) + (row.server_port ? ':' + row.server_port : '');
                return 'Локальный';
              },
            },
            {
              title: 'TLS',
              width: 70,
              align: 'center',
              render: (_: unknown, row: JsonObject) =>
                row.tls ? <Tag color="green">TLS</Tag> : '—',
            },
            {
              title: 'Проверка',
              width: 100,
              render: (_: unknown, row: JsonObject) => {
                const name = asString(row.tag);
                const broken = singBoxHealthIssues(config).some((issue) =>
                  issue.includes('"' + name + '"'),
                );
                return broken ? <Tag color="error">Проверить</Tag> : <Tag color="success">ОК</Tag>;
              },
            },
            {
              title: '',
              width: 180,
              render: (_: unknown, _row: JsonObject, index: number) => (
                <Space>
                  <Button
                    size="small"
                    onClick={() => {
                      setEditingOutbound(index);
                      setOutboundModalOpen(true);
                    }}
                  >
                    Изменить
                  </Button>
                  <JsonModal
                    title={(asString(items[index].tag) || 'Outbound') + ' — JSON'}
                    value={items[index]}
                    onApply={(next) => {
                      const copy = [...items];
                      copy[index] = asObject(next);
                      updateSection('outbounds', copy);
                    }}
                  />
                  <Button
                    size="small"
                    danger
                    icon={<DeleteOutlined />}
                    onClick={() =>
                      updateSection(
                        'outbounds',
                        items.filter((_, i) => i !== index),
                      )
                    }
                  />
                </Space>
              ),
            },
          ]}
        />
        <div className="singbox-inline-actions">
          <Button
            icon={<ExportOutlined />}
            onClick={() => {
              const blob = new Blob([JSON.stringify(items, null, 2)], { type: 'application/json' });
              const url = URL.createObjectURL(blob);
              const anchor = document.createElement('a');
              anchor.href = url;
              anchor.download = 'singbox-outbounds.json';
              anchor.click();
              URL.revokeObjectURL(url);
            }}
          >
            Экспорт JSON
          </Button>
          <JsonModal
            title="Все outbounds"
            value={items}
            onApply={(next) => updateSection('outbounds', Array.isArray(next) ? next : [])}
            buttonText="Массовый JSON"
          />
        </div>{' '}
      </Card>
    );
  };

  const renderRoute = () => {
    const value = asObject(sectionValue('route', config));
    const rules = Array.isArray(value.rules) ? value.rules.map(asObject) : [];
    const outTags = asObjectArray(config.outbounds)
      .map((item) => asString(item.tag))
      .filter(Boolean);

    return (
      <>
        <Card>
          <div className="singbox-section-header">
            <div>
              <div className="singbox-section-title">Маршрутизация</div>
              <div className="singbox-section-description">
                Фильтр → назначение. Домены, IP, порты, protocol/network и inbound собираются
                формой, без запоминания JSON-ключей.
              </div>
            </div>
            <Button
              type="primary"
              icon={<PlusOutlined />}
              onClick={() => {
                setEditingRouteRule(null);
                setRouteRuleModalOpen(true);
              }}
            >
              Добавить правило
            </Button>
          </div>
          {showAdvanced && (
            <Row gutter={[12, 16]}>
              <Field label="Выход по умолчанию">
                <Select
                  allowClear
                  value={asString(value.final) || undefined}
                  options={outTags.map((v) => ({ value: v, label: v }))}
                  style={{ width: '100%' }}
                  onChange={(v) => patchSection('route', { final: v })}
                />
              </Field>
              <Field label="DNS для адресов серверов">
                <TextField
                  value={asString(value.default_domain_resolver)}
                  onChange={(v) => patchSection('route', { default_domain_resolver: v })}
                />
              </Field>
              <Field label="Default HTTP client">
                <TextField
                  value={asString(value.default_http_client)}
                  onChange={(v) => patchSection('route', { default_http_client: v })}
                />
              </Field>
              <Field label="Auto detect interface">
                <ToggleField
                  checked={asBoolean(value.auto_detect_interface)}
                  onChange={(v) => patchSection('route', { auto_detect_interface: v })}
                />
              </Field>
              <Field label="Find process">
                <ToggleField
                  checked={asBoolean(value.find_process)}
                  onChange={(v) => patchSection('route', { find_process: v })}
                />
              </Field>
              <Field label="Find neighbor">
                <ToggleField
                  checked={asBoolean(value.find_neighbor)}
                  onChange={(v) => patchSection('route', { find_neighbor: v })}
                />
              </Field>
            </Row>
          )}

          <Divider />
          <Table
            size="small"
            pagination={false}
            rowKey={(_, index) => String(index)}
            dataSource={rules}
            locale={{
              emptyText: (
                <Empty description="Правил нет. Используется выбранный выход в интернет." />
              ),
            }}
            columns={[
              {
                title: '#',
                width: 50,
                render: (_: unknown, _row: JsonObject, index: number) => index + 1,
              },
              {
                title: 'Условия',
                render: (_: unknown, row: JsonObject) => {
                  const pieces = [
                    asStringArray(row.domain).join(', '),
                    asStringArray(row.domain_suffix).join(', '),
                    asStringArray(row.ip_cidr).join(', '),
                    asStringArray(row.protocol).join(', '),
                    asStringArray(row.network).join(', '),
                    asStringArray(row.inbound).join(', '),
                  ].filter(Boolean);
                  return pieces.length ? pieces.join(' · ') : 'Любой трафик';
                },
              },
              {
                title: 'Outbound',
                width: 160,
                render: (_: unknown, row: JsonObject) =>
                  asString(row.outbound) ? <Tag color="blue">{asString(row.outbound)}</Tag> : '—',
              },
              {
                title: 'Комментарий',
                render: (_: unknown, row: JsonObject) => asString(row.comment) || '—',
              },
              {
                title: '',
                width: 150,
                render: (_: unknown, _row: JsonObject, index: number) => (
                  <Space>
                    <Button
                      size="small"
                      onClick={() => {
                        setEditingRouteRule(index);
                        setRouteRuleModalOpen(true);
                      }}
                    >
                      Изменить
                    </Button>
                    <Button
                      size="small"
                      danger
                      icon={<DeleteOutlined />}
                      onClick={() =>
                        patchSection('route', { rules: rules.filter((_, i) => i !== index) })
                      }
                    />
                  </Space>
                ),
              },
            ]}
          />
        </Card>

        {showAdvanced && (
          <Card>
            <div className="singbox-card-title">Rule-set и расширенные правила</div>
            <div className="singbox-section-description">
              Сложные наборы остаются редактируемыми вручную, но основная маршрутизация больше не
              требует ручной сборки JSON.
            </div>
            <div className="singbox-inline-actions">
              <JsonModal
                title="Route rules"
                value={rules}
                onApply={(next) =>
                  patchSection('route', { rules: Array.isArray(next) ? next : [] })
                }
                buttonText="Все rules"
              />
              <JsonModal
                title="Rule-set"
                value={Array.isArray(value.rule_set) ? value.rule_set : []}
                onApply={(next) =>
                  patchSection('route', { rule_set: Array.isArray(next) ? next : [] })
                }
                buttonText="Rule-set"
              />
              <JsonModal
                title="Route"
                value={value}
                onApply={(next) => updateSection('route', asObject(next))}
                buttonText="Расширенный JSON"
              />
            </div>
          </Card>
        )}
      </>
    );
  };

  const renderExperimental = () => {
    const value = asObject(sectionValue('experimental', config));
    const cacheFile = asObject(value.cache_file);
    const clash = asObject(value.clash_api);
    const v2ray = asObject(value.v2ray_api);
    const patchNested = (key: string, patch: JsonObject) =>
      patchSection('experimental', { [key]: { ...asObject(value[key]), ...patch } });
    return (
      <>
        <SectionHeader title="Experimental" description="Кэш-файл и совместимые API." />
        <Row gutter={[12, 12]}>
          <Col xs={24} lg={12}>
            <Card title="Cache file">
              <Row gutter={[12, 12]}>
                <Field label="Включён" span={24}>
                  <ToggleField
                    checked={asBoolean(cacheFile.enabled)}
                    onChange={(next) => patchNested('cache_file', { enabled: next })}
                  />
                </Field>
                <Field label="Path" span={24}>
                  <TextField
                    value={asString(cacheFile.path)}
                    onChange={(next) => patchNested('cache_file', { path: next })}
                  />
                </Field>
                <Field label="Cache ID" span={24}>
                  <TextField
                    value={asString(cacheFile.cache_id)}
                    onChange={(next) => patchNested('cache_file', { cache_id: next })}
                  />
                </Field>
                <Field label="Store fakeip">
                  <ToggleField
                    checked={asBoolean(cacheFile.store_fakeip)}
                    onChange={(next) => patchNested('cache_file', { store_fakeip: next })}
                  />
                </Field>
                <Field label="Store DNS">
                  <ToggleField
                    checked={asBoolean(cacheFile.store_dns)}
                    onChange={(next) => patchNested('cache_file', { store_dns: next })}
                  />
                </Field>
              </Row>
              <div className="singbox-item-actions">
                <JsonModal
                  title="Cache file — JSON"
                  value={cacheFile}
                  onApply={(next) =>
                    patchSection('experimental', { ...value, cache_file: asObject(next) })
                  }
                />
              </div>
            </Card>
          </Col>
          <Col xs={24} lg={12}>
            <Card title="Clash API">
              <Row gutter={[12, 12]}>
                <Field label="External controller" span={24}>
                  <TextField
                    value={asString(clash.external_controller)}
                    onChange={(next) => patchNested('clash_api', { external_controller: next })}
                  />
                </Field>
                <Field label="External UI" span={24}>
                  <TextField
                    value={asString(clash.external_ui)}
                    onChange={(next) => patchNested('clash_api', { external_ui: next })}
                  />
                </Field>
                <Field label="Secret" span={24}>
                  <Input.Password
                    value={asString(clash.secret)}
                    onChange={(event) => patchNested('clash_api', { secret: event.target.value })}
                  />
                </Field>
                <Field label="Allow private network">
                  <ToggleField
                    checked={asBoolean(clash.access_control_allow_private_network)}
                    onChange={(next) =>
                      patchNested('clash_api', { access_control_allow_private_network: next })
                    }
                  />
                </Field>
              </Row>
              <div className="singbox-item-actions">
                <JsonModal
                  title="Clash API — JSON"
                  value={clash}
                  onApply={(next) =>
                    patchSection('experimental', { ...value, clash_api: asObject(next) })
                  }
                />
              </div>
            </Card>
          </Col>
          <Col xs={24}>
            <Card title="V2Ray API">
              <Row gutter={[12, 12]}>
                <Field label="Listen" span={24}>
                  <TextField
                    value={asString(v2ray.listen)}
                    onChange={(next) => patchNested('v2ray_api', { listen: next })}
                  />
                </Field>
              </Row>
              <div className="singbox-item-actions">
                <JsonModal
                  title="V2Ray API — JSON"
                  value={v2ray}
                  onApply={(next) =>
                    patchSection('experimental', { ...value, v2ray_api: asObject(next) })
                  }
                />
              </div>
            </Card>
          </Col>
        </Row>
      </>
    );
  };

  const renderSchema = () => (
    <Card>
      <SectionHeader
        title="Schema"
        description="URL JSON Schema, используемая для подсказок и проверки структуры конфигурации."
        onReset={() => updateSection('schema', sectionFallbacks.schema)}
      />
      <TextField
        value={asString(sectionValue('schema', config))}
        onChange={(next) => updateSection('schema', next)}
        placeholder="https://sing-box.sagernet.org/schema.json"
      />
    </Card>
  );

  const renderBasic = () => (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {renderLog()}
      {renderNtp()}
      {renderSchema()}
    </Space>
  );

  const renderCertificates = () => (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {renderCertificate()}
      {renderCertificateProviders()}
    </Space>
  );

  const renderNetwork = () => (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      {renderHttpClients()}
      {renderNamespaces()}
    </Space>
  );

  const renderAdvanced = () => (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Card>
        <SectionHeader
          title="Расширенная конфигурация"
          description="Редкие параметры и полный JSON доступны только здесь, чтобы основной интерфейс не был перегружен."
        />
        <Space wrap>
          <JsonModal
            title="Вся конфигурация sing-box"
            value={config}
            onApply={(next) => setConfig(asObject(next))}
          />
          <Button icon={<ReloadOutlined />} onClick={() => requestRefresh()} disabled={saving}>
            Обновить из файла
          </Button>
        </Space>
      </Card>
      <Collapse
        items={[
          { key: 'basic', label: 'Журнал, время и схема', children: renderBasic() },
          {
            key: 'experimental',
            label: 'Кэш-файл и служебные API',
            children: renderExperimental(),
          },
          { key: 'certificates', label: 'Сертификаты', children: renderCertificates() },
          { key: 'network', label: 'Сеть и HTTP-клиенты', children: renderNetwork() },
          { key: 'endpoints', label: 'Туннели (Endpoints)', children: renderEndpoints() },
        ]}
      />
    </Space>
  );

  const healthIssues = singBoxHealthIssues(config);
  const dirty = !!snapshot && JSON.stringify(snapshot.config) !== JSON.stringify(config);

  const sectionBody = (() => {
    switch (activeSection) {
      case 'dns':
        return showAdvanced ? (
          renderDns()
        ) : (
          <SimpleSettings
            config={config}
            onChange={setConfig}
            section="dns"
            onNavigate={navigate}
          />
        );
      case 'routing':
        return (
          <Tabs
            activeKey={searchParams.get('routingTab') === 'adblock' ? 'adblock' : 'rules'}
            onChange={(key) => {
              const next = new URLSearchParams(location.search);
              next.set('routingTab', key);
              navigate(
                { pathname: location.pathname, search: next.toString(), hash: location.hash },
                { preventScrollReset: true },
              );
            }}
            items={[
              {
                key: 'rules',
                label: 'Маршрутизация',
                children: showAdvanced ? (
                  renderRoute()
                ) : (
                  <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                    <SimpleSettings
                      config={config}
                      onChange={setConfig}
                      section="routing"
                      onNavigate={navigate}
                    />
                    <Collapse
                      items={[
                        {
                          key: 'rules',
                          label: 'Правила для отдельных сайтов и подключений',
                          children: renderRoute(),
                        },
                      ]}
                    />
                  </Space>
                ),
              },
              { key: 'adblock', label: 'AdBlock', children: <AdBlockTab /> },
            ]}
          />
        );
      case 'outbound':
        return renderOutbounds();
      case 'endpoints':
        return renderEndpoints();
      case 'certificates':
        return renderCertificates();
      case 'network':
        return renderNetwork();
      case 'advanced':
        return renderAdvanced();
      default:
        return showAdvanced ? (
          renderBasic()
        ) : (
          <SimpleSettings config={config} onChange={setConfig} onNavigate={navigate} />
        );
    }
  })();

  const scrollTarget = () => document.getElementById('content-layout') || window;
  const requestRefresh = () => {
    if (!dirty) {
      void refresh();
      return;
    }
    modalApi.confirm({
      title: 'Отменить несохранённые изменения?',
      content: 'Будут загружены последние сохранённые настройки.',
      okText: 'Отменить изменения',
      cancelText: 'Продолжить настройку',
      onOk: refresh,
    });
  };
  const requestReset = () =>
    modalApi.confirm({
      title: 'Вернуть автоматические настройки sing-box?',
      content:
        'Ручные настройки DNS, исходящих подключений и маршрутизации будут сброшены. Входящие подключения и пользователи сохранятся.',
      okText: 'Сбросить настройки',
      cancelText: 'Отмена',
      okButtonProps: { danger: true },
      onOk: reset,
    });
  const visibleSections = showAdvanced
    ? Array.from(sectionKeys)
    : ['basic', 'dns', 'routing', 'outbound'];

  return (
    <ConfigProvider theme={antdThemeConfig}>
      {contextHolder}
      {modalContextHolder}
      <Layout
        className={`singbox-page ${isDark ? 'is-dark ' : ''}${isUltra ? 'is-ultra' : ''}`.trim()}
      >
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <FloatButton.BackTop target={scrollTarget} visibilityHeight={200} />
            <Spin spinning={loading || saving} delay={150} description={t('loading')} size="large">
              {fetchError ? (
                <Result
                  status="error"
                  title={t('somethingWentWrong')}
                  subTitle={fetchError}
                  extra={
                    <Button type="primary" onClick={() => void refresh()}>
                      {t('check')}
                    </Button>
                  }
                />
              ) : snapshot ? (
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Card className="singbox-toolbar">
                    <div className="singbox-toolbar-heading">
                      <strong>Настройки sing-box · {SECTION_LABELS[activeSection]}</strong>
                      <span>Сохранение применит настройки и перезапустит работающее ядро.</span>
                    </div>
                    <Row gutter={[12, 12]} align="middle">
                      <Col xs={24} md={14}>
                        <Space wrap>
                          <Button
                            icon={<SaveOutlined />}
                            type="primary"
                            loading={saving}
                            disabled={!dirty || loading}
                            onClick={save}
                          >
                            {t('pages.singBox.save')}
                          </Button>
                          <Button
                            icon={<ReloadOutlined />}
                            loading={saving}
                            onClick={requestRefresh}
                          >
                            {t('pages.singBox.refresh')}
                          </Button>
                          <Space>
                            <span>Расширенный режим</span>
                            <Switch
                              aria-label="Расширенный режим"
                              checked={showAdvanced}
                              disabled={saving}
                              onChange={(checked) => {
                                setAdvancedMode(checked);
                                if (!checked && expertSection) navigate('/singbox#basic');
                              }}
                            />
                          </Space>
                          {showAdvanced && (
                            <Button danger disabled={saving} onClick={requestReset}>
                              {t('pages.singBox.reset')}
                            </Button>
                          )}
                        </Space>
                      </Col>
                      <Col xs={24} md={10}>
                        <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                          <Space wrap>
                            <Tag color={snapshot?.running ? 'success' : 'default'}>
                              {snapshot?.running
                                ? t('pages.singBox.running')
                                : t('pages.singBox.stopped')}
                            </Tag>
                            {snapshot?.version && <Tag>{snapshot.version}</Tag>}
                            {showAdvanced && (
                              <Tag>
                                {snapshot?.configSource === 'disk'
                                  ? t('pages.singBox.sourceDisk')
                                  : t('pages.singBox.sourceGenerated')}
                              </Tag>
                            )}
                            {dirty && <Tag color="warning">Есть несохранённые изменения</Tag>}
                          </Space>
                        </div>
                      </Col>
                    </Row>
                  </Card>

                  {healthIssues.length > 0 && (
                    <Alert
                      type="warning"
                      showIcon
                      message={'Проверка конфигурации: ' + healthIssues.length + ' предупреждений'}
                      description={
                        <ul className="singbox-issue-list">
                          {healthIssues.slice(0, 8).map((issue) => (
                            <li key={issue}>{issue}</li>
                          ))}
                          {healthIssues.length > 8 && <li>И ещё {healthIssues.length - 8}...</li>}
                        </ul>
                      }
                    />
                  )}

                  {showAdvanced && (
                    <Alert
                      type="info"
                      showIcon
                      title={t('pages.singBox.actualTitle')}
                      description={
                        <Space orientation="vertical" size={0}>
                          <span>{snapshot?.configPath}</span>
                          {snapshot?.configModified && <span>{snapshot.configModified}</span>}
                        </Space>
                      }
                    />
                  )}

                  {showAdvanced && (
                    <Alert
                      type="info"
                      showIcon
                      title={t('pages.singBox.managedTitle')}
                      description={t('pages.singBox.managedDesc')}
                    />
                  )}

                  <Tabs
                    activeKey={activeSection}
                    onChange={(key) => {
                      if (sectionKeys.has(key)) navigate('/singbox#' + key);
                    }}
                    className="singbox-main-tabs"
                    items={visibleSections.map((key) => ({
                      key,
                      label: SECTION_LABELS[key],
                      children: key === activeSection ? sectionBody : null,
                    }))}
                  />
                </Space>
              ) : (
                <div style={{ minHeight: 200 }} />
              )}
            </Spin>
          </Layout.Content>
        </Layout>
      </Layout>

      <Modal
        open={shareLinkOpen}
        title="Импорт outbound по ссылке"
        okText="Добавить"
        cancelText="Отмена"
        onCancel={() => setShareLinkOpen(false)}
        onOk={() => {
          let parsed: JsonObject;
          try {
            parsed = parseShareLink(shareLink);
          } catch (error) {
            messageApi.error(error instanceof Error ? error.message : String(error));
            return;
          }
          const current = asObjectArray(sectionValue('outbounds', config));
          const tag = uniqueTag(
            asString(parsed.tag),
            [...current, ...asObjectArray(config.endpoints)].map((item) => asString(item.tag)),
          );
          const errors = outboundErrors({ ...parsed, tag }, []);
          if (errors.length) {
            messageApi.error(errors[0]);
            return;
          }
          updateSection('outbounds', [...current, { ...parsed, tag }]);
          setShareLink('');
          setShareLinkOpen(false);
          messageApi.success('Outbound создан автоматически.');
        }}
      >
        <Alert
          type="info"
          showIcon
          message="Автоматический импорт"
          description="TLS, Reality и базовый WebSocket/gRPC transport переносятся из VLESS/Trojan ссылок."
          style={{ marginBottom: 12 }}
        />
        <Input.TextArea
          value={shareLink}
          onChange={(e) => setShareLink(e.target.value)}
          autoSize={{ minRows: 5, maxRows: 10 }}
          placeholder="vless://..."
        />
      </Modal>

      <SingBoxOutboundModal
        open={outboundModalOpen}
        value={
          editingOutbound == null
            ? null
            : asObjectArray(sectionValue('outbounds', config))[editingOutbound]
        }
        existingTags={[
          ...asObjectArray(sectionValue('outbounds', config)),
          ...asObjectArray(config.endpoints),
        ]
          .map((item) => asString(item.tag))
          .filter(Boolean)}
        onCancel={() => setOutboundModalOpen(false)}
        onSave={(next) => {
          const current = asObjectArray(sectionValue('outbounds', config));
          if (editingOutbound == null) current.push(next);
          else current[editingOutbound] = next;
          updateSection('outbounds', current);
          setOutboundModalOpen(false);
        }}
      />

      <SingBoxRouteRuleModal
        open={routeRuleModalOpen}
        value={
          editingRouteRule == null
            ? null
            : asObjectArray(asObject(sectionValue('route', config)).rules)[editingRouteRule]
        }
        inboundTags={
          snapshot?.inboundTags ??
          asObjectArray(config.inbounds)
            .map((item) => asString(item.tag))
            .filter(Boolean)
        }
        outboundTags={asObjectArray(sectionValue('outbounds', config))
          .map((item) => asString(item.tag))
          .filter(Boolean)}
        ruleSetTags={(() => {
          const ruleSet = asObject(sectionValue('route', config)).rule_set;
          return Array.isArray(ruleSet)
            ? ruleSet
                .map(asObject)
                .map((item) => asString(item.tag))
                .filter(Boolean)
            : [];
        })()}
        onCancel={() => setRouteRuleModalOpen(false)}
        onSave={(next) => {
          const routeValue = asObject(sectionValue('route', config));
          const rules = Array.isArray(routeValue.rules) ? routeValue.rules.map(asObject) : [];
          if (editingRouteRule == null) rules.push(next);
          else rules[editingRouteRule] = next;
          updateSection('route', { ...routeValue, rules });
          setRouteRuleModalOpen(false);
        }}
      />
    </ConfigProvider>
  );
}
