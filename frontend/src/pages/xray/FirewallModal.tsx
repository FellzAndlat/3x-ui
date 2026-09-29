import { useMemo, useState } from 'react';
import {
  Alert,
  Button,
  Divider,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd';
import { useTranslation } from 'react-i18next';

import { HttpUtil } from '@/utils';

type FirewallRule = {
  port?: number;
  portRange?: string;
  protocol: 'tcp' | 'udp' | string;
  source: string;
  label: string;
  owned: boolean;
  exists: boolean;
};

type FirewallManualRule = {
  port: number;
  protocol: 'tcp' | 'udp' | string;
  label?: string;
};

type FirewallStatus = {
  supported: boolean;
  backend: string;
  enabled: boolean;
  autoSync: boolean;
  rules: FirewallRule[];
  manualRules: FirewallManualRule[];
  message?: string;
};

type Props = {
  open: boolean;
  onClose: () => void;
};

function rulePort(rule: FirewallRule) {
  return rule.portRange || String(rule.port || '');
}

function rulePortStart(rule: FirewallRule) {
  if (rule.port) return rule.port;
  const value = Number.parseInt((rule.portRange || '').split(/[-:]/, 1)[0] || '', 10);
  return Number.isFinite(value) ? value : 0;
}

export function FirewallModal({ open, onClose }: Props) {
  const { i18n } = useTranslation();
  const ru = (i18n.resolvedLanguage || i18n.language || '').toLowerCase().startsWith('ru');
  const text = useMemo(
    () =>
      ru
        ? {
            title: 'Управление файрволлом',
            loading: 'Получение состояния файрволла…',
            unsupported:
              'Поддерживаемый файрволл не найден. Установите UFW, firewalld, nftables или iptables.',
            enabled: 'Файрволл включён',
            disabled: 'Файрволл выключен',
            auto: 'Автоматически открывать и закрывать порты подключений',
            autoHint:
              'При создании, изменении, включении или удалении inbound правила синхронизируются автоматически.',
            safety:
              'Порт панели, текущий внешний порт панели, порт подписок и SSH защищаются автоматически, чтобы не потерять доступ к серверу.',
            sync: 'Синхронизировать сейчас',
            rules: 'Активные и ожидаемые правила',
            manual: 'Ручные правила',
            add: 'Добавить',
            port: 'Порт',
            protocol: 'Протокол',
            label: 'Описание',
            labelPlaceholder: 'Например: DNS, мониторинг, игровой сервер',
            source: 'Назначение',
            state: 'Состояние',
            open: 'Открыт',
            missing: 'Не применён',
            managed: '3x-ui',
            external: 'Существующее',
            remove: 'Удалить',
            removeConfirm: 'Удалить это ручное правило?',
            noManual: 'Ручных правил нет',
            panel: 'Веб-панель',
            subscription: 'Подписки',
            session: 'Текущее подключение к панели',
            ssh: 'SSH',
            inbound: 'Inbound',
            manualSource: 'Ручное правило',
          }
        : {
            title: 'Firewall management',
            loading: 'Loading firewall status…',
            unsupported:
              'No supported firewall found. Install UFW, firewalld, nftables, or iptables.',
            enabled: 'Firewall enabled',
            disabled: 'Firewall disabled',
            auto: 'Automatically open and close inbound ports',
            autoHint:
              'Rules are synchronized after inbound create, update, enable, disable, and delete.',
            safety:
              'Panel, current external panel port, subscription port, and SSH are protected automatically to prevent lockout.',
            sync: 'Sync now',
            rules: 'Active and expected rules',
            manual: 'Manual rules',
            add: 'Add',
            port: 'Port',
            protocol: 'Protocol',
            label: 'Description',
            labelPlaceholder: 'For example: DNS, monitoring, game server',
            source: 'Purpose',
            state: 'State',
            open: 'Open',
            missing: 'Not applied',
            managed: '3x-ui',
            external: 'Existing',
            remove: 'Delete',
            removeConfirm: 'Delete this manual rule?',
            noManual: 'No manual rules',
            panel: 'Web panel',
            subscription: 'Subscriptions',
            session: 'Current panel connection',
            ssh: 'SSH',
            inbound: 'Inbound',
            manualSource: 'Manual rule',
          },
    [ru],
  );

  const [status, setStatus] = useState<FirewallStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [action, setAction] = useState('');
  const [port, setPort] = useState<number | null>(null);
  const [protocol, setProtocol] = useState('both');
  const [label, setLabel] = useState('');

  async function refresh() {
    setLoading(true);
    try {
      const msg = await HttpUtil.get<FirewallStatus>('/panel/api/server/firewall/status');
      if (msg.success && msg.obj) setStatus(msg.obj);
    } finally {
      setLoading(false);
    }
  }

  async function post(path: string, data?: Record<string, unknown>, key = path) {
    setAction(key);
    try {
      const msg = await HttpUtil.post<FirewallStatus>(path, data, { silentSuccess: true });
      if (msg.success && msg.obj) {
        setStatus(msg.obj);
        return true;
      }
      return false;
    } finally {
      setAction('');
    }
  }

  const sourceLabel = (rule: FirewallRule) => {
    const source =
      rule.source === 'panel'
        ? text.panel
        : rule.source === 'subscription'
          ? text.subscription
          : rule.source === 'session'
            ? text.session
            : rule.source === 'ssh'
              ? text.ssh
              : rule.source === 'manual'
                ? text.manualSource
                : text.inbound;
    return rule.label && rule.source === 'inbound' ? `${source}: ${rule.label}` : source;
  };

  const ruleColumns = [
    {
      title: text.port,
      key: 'port',
      width: 130,
      sorter: (a: FirewallRule, b: FirewallRule) => rulePortStart(a) - rulePortStart(b),
      render: (_: unknown, rule: FirewallRule) => rulePort(rule),
    },
    {
      title: text.protocol,
      dataIndex: 'protocol',
      width: 100,
      render: (value: string) => <Tag>{value.toUpperCase()}</Tag>,
    },
    {
      title: text.source,
      key: 'source',
      render: (_: unknown, rule: FirewallRule) => sourceLabel(rule),
    },
    {
      title: text.state,
      key: 'state',
      width: 190,
      render: (_: unknown, rule: FirewallRule) => (
        <Space size={4} wrap>
          <Tag color={rule.exists ? 'success' : 'warning'}>
            {rule.exists ? text.open : text.missing}
          </Tag>
          {rule.exists && <Tag>{rule.owned ? text.managed : text.external}</Tag>}
        </Space>
      ),
    },
  ];

  const manualColumns = [
    { title: text.port, dataIndex: 'port', width: 100 },
    {
      title: text.protocol,
      dataIndex: 'protocol',
      width: 110,
      render: (value: string) => <Tag>{value.toUpperCase()}</Tag>,
    },
    {
      title: text.label,
      dataIndex: 'label',
      ellipsis: true,
      render: (value?: string) => value || '—',
    },
    {
      title: '',
      key: 'delete',
      align: 'right' as const,
      render: (_: unknown, rule: FirewallManualRule) => (
        <Popconfirm
          title={text.removeConfirm}
          onConfirm={() =>
            void post(
              '/panel/api/server/firewall/rules/delete',
              { port: rule.port, protocol: rule.protocol },
              `delete-${rule.port}-${rule.protocol}`,
            )
          }
        >
          <Button danger size="small" loading={action === `delete-${rule.port}-${rule.protocol}`}>
            {text.remove}
          </Button>
        </Popconfirm>
      ),
    },
  ];

  const supported = status?.supported ?? false;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      afterOpenChange={(visible) => {
        if (visible) void refresh();
      }}
      footer={null}
      title={text.title}
      width={900}
    >
      {!status ? (
        <Typography.Text type="secondary">{text.loading}</Typography.Text>
      ) : !supported ? (
        <Alert
          type="warning"
          showIcon
          title={status.message || text.unsupported}
          description={text.unsupported}
        />
      ) : (
        <Space direction="vertical" size="middle" style={{ width: '100%' }}>
          <Space size="large" wrap>
            <Space>
              <Switch
                checked={Boolean(status.enabled)}
                loading={action === 'enabled'}
                onChange={(checked) =>
                  void post('/panel/api/server/firewall/enabled', { enabled: checked }, 'enabled')
                }
              />
              <Typography.Text strong>
                {status.enabled ? text.enabled : text.disabled}
              </Typography.Text>
            </Space>
            <Tag>{status.backend}</Tag>
            <Button
              loading={action === 'sync'}
              onClick={() => void post('/panel/api/server/firewall/sync', undefined, 'sync')}
            >
              {text.sync}
            </Button>
          </Space>

          <Alert type="info" showIcon title={text.safety} />

          <div>
            <Space align="start">
              <Switch
                checked={Boolean(status.autoSync)}
                loading={action === 'auto'}
                onChange={(checked) =>
                  void post('/panel/api/server/firewall/auto-sync', { enabled: checked }, 'auto')
                }
              />
              <div>
                <Typography.Text strong>{text.auto}</Typography.Text>
                <br />
                <Typography.Text type="secondary">{text.autoHint}</Typography.Text>
              </div>
            </Space>
          </div>

          <Divider titlePlacement="start">{text.rules}</Divider>
          <Table<FirewallRule>
            size="small"
            rowKey={(rule) => `${rulePort(rule)}-${rule.protocol}-${rule.source}`}
            columns={ruleColumns}
            dataSource={status.rules || []}
            pagination={false}
            scroll={{ x: 620 }}
          />

          <Divider titlePlacement="start">{text.manual}</Divider>
          <Space wrap>
            <InputNumber
              min={1}
              max={65535}
              value={port}
              placeholder={text.port}
              onChange={(value) => setPort(value)}
              style={{ width: 140 }}
            />
            <Select
              value={protocol}
              onChange={setProtocol}
              style={{ width: 130 }}
              options={[
                { value: 'both', label: 'TCP + UDP' },
                { value: 'tcp', label: 'TCP' },
                { value: 'udp', label: 'UDP' },
              ]}
            />
            <Input
              value={label}
              maxLength={120}
              placeholder={text.labelPlaceholder}
              onChange={(event) => setLabel(event.target.value)}
              style={{ width: 290 }}
            />
            <Button
              type="primary"
              disabled={!port}
              loading={action === 'add'}
              onClick={() => {
                if (!port) return;
                void post(
                  '/panel/api/server/firewall/rules/add',
                  { port, protocol, label },
                  'add',
                ).then((success) => {
                  if (success) {
                    setPort(null);
                    setLabel('');
                  }
                });
              }}
            >
              {text.add}
            </Button>
          </Space>
          <Table<FirewallManualRule>
            size="small"
            rowKey={(rule) => `${rule.port}-${rule.protocol}`}
            columns={manualColumns}
            dataSource={status.manualRules || []}
            pagination={false}
            locale={{ emptyText: text.noManual }}
          />
        </Space>
      )}
      {loading && status ? (
        <Typography.Text type="secondary"> {text.loading}</Typography.Text>
      ) : null}
    </Modal>
  );
}
