import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { Button, Dropdown, Input, Modal, Select, Space, Table, Tabs, message } from 'antd';
import {
  AimOutlined,
  ControlOutlined,
  ExportOutlined,
  ImportOutlined,
  MoreOutlined,
  PlusOutlined,
  SafetyOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons';

import { catTabLabel } from '@/pages/settings/catTabLabel';
import PromptModal from '@/components/feedback/PromptModal';
import TextModal from '@/components/feedback/TextModal';
import { isBalancerLoopbackTag } from '../balancers/balancer-loopback';
import { HttpUtil } from '@/utils';
import AdBlockTab from '@/pages/adblock/AdBlockTab';
import RoutingBasic from './RoutingBasic';
import RouteTester from './RouteTester';
import RuleFormModal from './RuleFormModal';
import type { RoutingRule } from './RuleFormModal';
import RuleCardList from './RuleCardList';
import { useRoutingColumns } from './useRoutingColumns';
import { arrJoin, originalRuleIndex } from './helpers';
import type { RuleRow } from './types';
import type { XraySettingsValue, SetTemplate } from '@/hooks/useXraySetting';
import type { RuleObject } from '@/schemas/routing';
import './RoutingTab.css';

interface RoutingTabProps {
  templateSettings: XraySettingsValue | null;
  setTemplateSettings: SetTemplate;
  inboundTags: string[];
  clientReverseTags: string[];
  subscriptionOutboundTags?: string[];
  isMobile: boolean;
}

export default function RoutingTab({
  templateSettings,
  setTemplateSettings,
  inboundTags,
  clientReverseTags,
  subscriptionOutboundTags,
  isMobile,
}: RoutingTabProps) {
  const { t } = useTranslation();
  const location = useLocation();
  const navigate = useNavigate();
  const searchParams = new URLSearchParams(location.search);
  const requestedTab = searchParams.get('routingTab');
  const activeTab = ['basic', 'rules', 'tester', 'adblock'].includes(requestedTab || '')
    ? requestedTab!
    : 'basic';
  const [modal, modalContextHolder] = Modal.useModal();
  const [ruleModalOpen, setRuleModalOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<RoutingRule | null>(null);
  const [editingIndex, setEditingIndex] = useState<number | null>(null);
  const [draggedIndex, setDraggedIndex] = useState<number | null>(null);
  const [dropTargetIndex, setDropTargetIndex] = useState<number | null>(null);

  const [routingPresets, setRoutingPresets] = useState<
    { id: number; name: string; description: string; ruleCount: number }[]
  >([]);
  const [presetId, setPresetId] = useState<number | null>(null);
  const [presetSaveOpen, setPresetSaveOpen] = useState(false);
  const [presetName, setPresetName] = useState('');
  const [presetDescription, setPresetDescription] = useState('');
  const [presetBusy, setPresetBusy] = useState(false);
  const dragRef = useRef<{
    from: number | null;
    to: number | null;
    startY: number;
    moved: boolean;
  }>({
    from: null,
    to: null,
    startY: 0,
    moved: false,
  });

  const rules = useMemo(
    () => (templateSettings?.routing?.rules || []) as RoutingRule[],
    [templateSettings?.routing?.rules],
  );
  const rulesRef = useRef(rules);
  const rowsRef = useRef<RuleRow[]>([]);

  const rows: RuleRow[] = useMemo(
    () =>
      rules
        .map((rule, idx) => {
          const r: RuleRow = { key: idx };
          r.enabled = rule.enabled !== false;
          r.domain = arrJoin(rule.domain);
          r.ip = arrJoin(rule.ip);
          r.port = rule.port;
          r.sourcePort = rule.sourcePort;
          r.vlessRoute = rule.vlessRoute;
          r.network = rule.network;
          r.sourceIP = arrJoin(rule.sourceIP);
          r.user = arrJoin(rule.user);
          r.inboundTag = arrJoin(rule.inboundTag);
          r.protocol = arrJoin(rule.protocol);
          if (rule.attrs && typeof rule.attrs === 'object' && !Array.isArray(rule.attrs)) {
            r.attrs = JSON.stringify(rule.attrs, null, 2);
          }
          r.comment = rule.comment || undefined;
          r.outboundTag = rule.outboundTag;
          r.balancerTag = rule.balancerTag;
          return r;
        })
        .filter((r) => {
          const inboundTags = (rules[r.key]?.inboundTag || []) as string[];
          return !inboundTags.some(isBalancerLoopbackTag);
        }),
    [rules],
  );

  useEffect(() => {
    rulesRef.current = rules;
    rowsRef.current = rows;
  });

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      const msg = await HttpUtil.get<
        { id: number; name: string; description: string; ruleCount: number }[]
      >('/panel/api/xray/routingPresets/list', undefined, { silent: true });
      if (!cancelled && msg?.success) setRoutingPresets(msg.obj || []);
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const mutate = useCallback(
    (mutator: (next: XraySettingsValue) => void) => {
      setTemplateSettings((prev) => {
        if (!prev) return prev;
        const clone = JSON.parse(JSON.stringify(prev)) as XraySettingsValue;
        mutator(clone);
        return clone;
      });
    },
    [setTemplateSettings],
  );

  const refreshRoutingPresets = useCallback(async () => {
    const msg = await HttpUtil.get<
      { id: number; name: string; description: string; ruleCount: number }[]
    >('/panel/api/xray/routingPresets/list', undefined, { silent: true });
    if (msg?.success) setRoutingPresets(msg.obj || []);
  }, []);

  const applyRoutingPreset = useCallback(
    async (id: number) => {
      setPresetBusy(true);
      try {
        const msg = await HttpUtil.get<{
          id: number;
          name: string;
          description: string;
          rules: RuleObject[];
        }>('/panel/api/xray/routingPresets/get/' + id);
        if (!msg?.success || !msg.obj) {
          message.error(msg?.msg || t('pages.xray.routingPresetLoadFailed'));
          return;
        }
        mutate((tt) => {
          if (!tt.routing) tt.routing = { rules: [] };
          tt.routing.rules = JSON.parse(JSON.stringify(msg.obj!.rules)) as RuleObject[];
        });
        setPresetId(id);
        message.success(t('pages.xray.routingPresetApplied'));
      } finally {
        setPresetBusy(false);
      }
    },
    [mutate, t],
  );

  const saveRoutingPreset = useCallback(async () => {
    if (!presetName.trim()) {
      message.warning(t('pages.xray.routingPresetNameRequired'));
      return;
    }
    setPresetBusy(true);
    try {
      const msg = await HttpUtil.post<{ id: number }>(
        '/panel/api/xray/routingPresets/save',
        {
          name: presetName.trim(),
          description: presetDescription.trim(),
          rules: JSON.parse(JSON.stringify(rules)),
        },
        { headers: { 'Content-Type': 'application/json' } },
      );
      if (!msg?.success) {
        message.error(msg?.msg || t('pages.xray.routingPresetSaveFailed'));
        return;
      }
      setPresetSaveOpen(false);
      setPresetName('');
      setPresetDescription('');
      await refreshRoutingPresets();
      message.success(t('pages.xray.routingPresetSaved'));
    } finally {
      setPresetBusy(false);
    }
  }, [presetDescription, presetName, refreshRoutingPresets, rules, t]);

  const deleteRoutingPreset = useCallback(
    async (id: number) => {
      setPresetBusy(true);
      try {
        const msg = await HttpUtil.post('/panel/api/xray/routingPresets/del/' + id);
        if (!msg?.success) {
          message.error(msg?.msg || t('pages.xray.routingPresetDeleteFailed'));
          return;
        }
        if (presetId === id) setPresetId(null);
        await refreshRoutingPresets();
        message.success(t('pages.xray.routingPresetDeleted'));
      } finally {
        setPresetBusy(false);
      }
    },
    [presetId, refreshRoutingPresets, t],
  );

  const inboundTagOptions = useMemo(() => {
    const seen = new Set<string>();
    const out: string[] = [];
    const push = (tag?: string) => {
      if (!tag || seen.has(tag) || isBalancerLoopbackTag(tag)) return;
      seen.add(tag);
      out.push(tag);
    };
    for (const ib of (templateSettings?.inbounds as Array<{ tag?: string }>) || []) push(ib?.tag);
    for (const tag of inboundTags || []) push(tag);
    for (const ob of templateSettings?.outbounds || []) {
      const obx = ob as {
        reverse?: { tag?: string };
        settings?: { reverse?: { tag?: string }; inboundTag?: string };
      };
      push(obx?.reverse?.tag || obx?.settings?.reverse?.tag || obx?.settings?.inboundTag);
    }
    push((templateSettings?.dns as { tag?: string } | undefined)?.tag);
    for (const s of (templateSettings?.dns as { servers?: Array<{ tag?: string }> } | undefined)
      ?.servers || []) {
      if (typeof s === 'object' && s?.tag) push(s.tag);
    }
    return out;
  }, [templateSettings, inboundTags]);

  const outboundTagOptions = useMemo(() => {
    const out = new Set<string>(['']);
    for (const ob of templateSettings?.outbounds || []) {
      if (ob?.tag) out.add(ob.tag);
    }
    for (const tag of clientReverseTags || []) {
      if (tag) out.add(tag);
    }
    for (const tag of subscriptionOutboundTags || []) {
      if (tag) out.add(tag);
    }
    return [...out];
  }, [templateSettings?.outbounds, clientReverseTags, subscriptionOutboundTags]);

  const balancerTagOptions = useMemo(() => {
    const out: string[] = [''];
    for (const b of (templateSettings?.routing?.balancers as Array<{ tag?: string }>) || []) {
      if (b?.tag) out.push(b.tag);
    }
    return out;
  }, [templateSettings?.routing?.balancers]);

  const [importOpen, setImportOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);
  const [exportContent, setExportContent] = useState('');

  function exportRules() {
    setExportContent(JSON.stringify(rules, null, 2));
    setExportOpen(true);
  }

  function importRules(value: string) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(value);
    } catch {
      message.error(t('pages.xray.importInvalidJson'));
      return;
    }
    const obj = parsed as { rules?: unknown; routing?: { rules?: unknown } };
    const list = Array.isArray(parsed)
      ? parsed
      : Array.isArray(obj?.rules)
        ? obj.rules
        : Array.isArray(obj?.routing?.rules)
          ? obj.routing!.rules
          : null;
    if (!list) {
      message.error(t('pages.xray.importInvalidJson'));
      return;
    }
    mutate((tt) => {
      if (!tt.routing) tt.routing = { rules: [] };
      if (!Array.isArray(tt.routing.rules)) tt.routing.rules = [];
      tt.routing.rules.push(...(list as RuleObject[]));
    });
    setImportOpen(false);
  }

  function openAdd() {
    setEditingRule(null);
    setEditingIndex(null);
    setRuleModalOpen(true);
  }
  function openEdit(idx: number) {
    const target = originalRuleIndex(rowsRef.current, idx);
    setEditingRule(rulesRef.current[target]);
    setEditingIndex(target);
    setRuleModalOpen(true);
  }
  function onRuleConfirm(rule: Record<string, unknown>) {
    if (JSON.stringify(rule).length <= 3) {
      setRuleModalOpen(false);
      return;
    }
    mutate((tt) => {
      if (!tt.routing) tt.routing = { rules: [] };
      if (!Array.isArray(tt.routing.rules)) tt.routing.rules = [];
      const typed = rule as unknown as RuleObject;
      if (editingIndex == null) tt.routing.rules.push(typed);
      else tt.routing.rules[editingIndex] = typed;
    });
    setRuleModalOpen(false);
  }

  function confirmDelete(idx: number) {
    const target = originalRuleIndex(rowsRef.current, idx);
    modal.confirm({
      title: `${t('delete')} ${t('pages.xray.Routings')} #${idx + 1}?`,
      okText: t('delete'),
      okType: 'danger',
      cancelText: t('cancel'),
      onOk: () =>
        mutate((tt) => {
          tt.routing?.rules?.splice(target, 1);
        }),
    });
  }

  function moveUp(idx: number) {
    if (idx <= 0) return;
    const target = originalRuleIndex(rowsRef.current, idx);
    const prev = originalRuleIndex(rowsRef.current, idx - 1);
    mutate((tt) => {
      const list = tt.routing?.rules;
      if (!list || !list[target] || !list[prev]) return;
      [list[prev], list[target]] = [list[target], list[prev]];
    });
  }
  function moveDown(idx: number) {
    if (idx >= rowsRef.current.length - 1) return;
    const target = originalRuleIndex(rowsRef.current, idx);
    const next = originalRuleIndex(rowsRef.current, idx + 1);
    mutate((tt) => {
      const list = tt.routing?.rules;
      if (!list || !list[target] || !list[next]) return;
      [list[next], list[target]] = [list[target], list[next]];
    });
  }
  function toggleRule(idx: number, enabled: boolean) {
    const target = originalRuleIndex(rowsRef.current, idx);
    mutate((tt) => {
      const list = tt.routing?.rules;
      if (!list || !list[target]) return;
      list[target].enabled = enabled;
    });
  }

  function onHandlePointerDown(idx: number, ev: React.PointerEvent) {
    if (ev.button != null && ev.button !== 0) return;
    ev.preventDefault();
    try {
      (ev.currentTarget as Element).setPointerCapture(ev.pointerId);
    } catch {
      /* ignore */
    }
    dragRef.current = { from: idx, to: idx, startY: ev.clientY, moved: false };
    setDraggedIndex(idx);
    setDropTargetIndex(idx);

    const onMove = (e: PointerEvent) => {
      const state = dragRef.current;
      if (state.from == null) return;
      if (!state.moved && Math.abs(e.clientY - state.startY) < 5) return;
      state.moved = true;
      const el = document.elementFromPoint(e.clientX, e.clientY);
      if (!el) return;
      const target = el.closest('[data-row-key]');
      if (!target) return;
      const newIdx = Number(target.getAttribute('data-row-key'));
      if (Number.isFinite(newIdx) && newIdx !== state.to) {
        state.to = newIdx;
        setDropTargetIndex(newIdx);
      }
    };

    const onUp = () => {
      document.removeEventListener('pointermove', onMove);
      document.removeEventListener('pointerup', onUp);
      document.removeEventListener('pointercancel', onUp);
      const { from, to, moved } = dragRef.current;
      dragRef.current = { from: null, to: null, startY: 0, moved: false };
      setDraggedIndex(null);
      setDropTargetIndex(null);
      if (!moved || from == null || to == null || from === to) return;
      const fromOrig = originalRuleIndex(rowsRef.current, from);
      const toOrig = originalRuleIndex(rowsRef.current, to);
      mutate((tt) => {
        const list = tt.routing?.rules;
        if (!list) return;
        const [movedItem] = list.splice(fromOrig, 1);
        list.splice(toOrig, 0, movedItem);
      });
    };

    document.addEventListener('pointermove', onMove);
    document.addEventListener('pointerup', onUp);
    document.addEventListener('pointercancel', onUp);
  }

  const hasSource = rows.some((r) => r.sourceIP || r.sourcePort || r.vlessRoute);
  const hasBalancer = rows.some((r) => r.balancerTag);

  const desktopColumns = useRoutingColumns({
    isMobile,
    rowsLength: rows.length,
    showSource: hasSource,
    showBalancer: hasBalancer,
    onHandlePointerDown,
    openEdit,
    moveUp,
    moveDown,
    confirmDelete,
    toggleRule,
  });

  const tableScrollX = desktopColumns.reduce((sum, c) => {
    const col = c as { width?: number; hidden?: boolean };
    return col.hidden ? sum : sum + (typeof col.width === 'number' ? col.width : 0);
  }, 0);

  return (
    <>
      {modalContextHolder}
      <Tabs
        activeKey={activeTab}
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
            key: 'basic',
            label: catTabLabel(<ControlOutlined />, t('pages.xray.basicRouting'), isMobile),
            children: (
              <RoutingBasic
                templateSettings={templateSettings}
                setTemplateSettings={setTemplateSettings}
              />
            ),
          },
          {
            key: 'rules',
            label: catTabLabel(<UnorderedListOutlined />, t('pages.xray.Routings'), isMobile),
            children: (
              <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
                <Space wrap>
                  <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>
                    {t('pages.xray.Routings')}
                  </Button>
                  <Select
                    allowClear
                    value={presetId ?? undefined}
                    placeholder={t('pages.xray.routingPreset')}
                    loading={presetBusy}
                    style={{ minWidth: isMobile ? 160 : 220 }}
                    options={routingPresets.map((preset) => ({
                      value: preset.id,
                      label: `${preset.name} · ${preset.ruleCount}`,
                    }))}
                    onChange={(value) => {
                      if (value != null) void applyRoutingPreset(Number(value));
                    }}
                    dropdownRender={(menu) => (
                      <>
                        {menu}
                        <div
                          style={{
                            padding: '8px 12px',
                            borderTop: '1px solid var(--ant-color-border)',
                          }}
                        >
                          <Button
                            type="link"
                            block
                            onClick={() => {
                              setPresetName('');
                              setPresetDescription('');
                              setPresetSaveOpen(true);
                            }}
                          >
                            {t('pages.xray.saveRoutingPreset')}
                          </Button>
                        </div>
                      </>
                    )}
                  />
                  <Dropdown
                    trigger={['click']}
                    menu={{
                      items: [
                        {
                          key: 'import',
                          icon: <ImportOutlined />,
                          label: t('pages.xray.importRules'),
                          onClick: () => setImportOpen(true),
                        },
                        {
                          key: 'export',
                          icon: <ExportOutlined />,
                          label: t('pages.xray.exportRules'),
                          disabled: rules.length === 0,
                          onClick: exportRules,
                        },
                      ],
                    }}
                  >
                    <Button icon={<MoreOutlined />}>{t('more')}</Button>
                  </Dropdown>
                </Space>

                {isMobile ? (
                  <RuleCardList
                    rows={rows}
                    draggedIndex={draggedIndex}
                    dropTargetIndex={dropTargetIndex}
                    onHandlePointerDown={onHandlePointerDown}
                    openEdit={openEdit}
                    moveUp={moveUp}
                    moveDown={moveDown}
                    confirmDelete={confirmDelete}
                    toggleRule={toggleRule}
                  />
                ) : (
                  <Table
                    columns={desktopColumns}
                    dataSource={rows}
                    rowKey={(r) => r.key}
                    pagination={false}
                    scroll={{ x: tableScrollX }}
                    size="small"
                    className="routing-table"
                    onRow={(_record, index) => {
                      const classes: string[] = [];
                      const i = index ?? -1;
                      if (draggedIndex === i) classes.push('row-dragging');
                      if (dropTargetIndex === i && draggedIndex !== i && draggedIndex != null) {
                        classes.push(i > draggedIndex ? 'drop-after' : 'drop-before');
                      }
                      return {
                        className: classes.join(' '),
                        'data-row-key': i,
                      } as React.HTMLAttributes<HTMLElement>;
                    }}
                  />
                )}
              </Space>
            ),
          },
          {
            key: 'tester',
            label: catTabLabel(<AimOutlined />, t('pages.xray.routeTester'), isMobile),
            children: <RouteTester inboundTags={inboundTagOptions} isMobile={isMobile} />,
          },
          {
            key: 'adblock',
            label: catTabLabel(<SafetyOutlined />, 'AdBlock', isMobile),
            children: <AdBlockTab />,
          },
        ]}
      />

      {activeTab === 'rules' &&
        presetId != null &&
        routingPresets.some((p) => p.id === presetId) && (
          <Space style={{ marginTop: 8 }} wrap>
            <Button
              danger
              size="small"
              loading={presetBusy}
              onClick={() =>
                modal.confirm({
                  title: t('pages.xray.deleteRoutingPreset'),
                  okText: t('delete'),
                  okType: 'danger',
                  cancelText: t('cancel'),
                  onOk: () => deleteRoutingPreset(presetId),
                })
              }
            >
              {t('delete')}
            </Button>
          </Space>
        )}
      <RuleFormModal
        open={ruleModalOpen}
        rule={editingRule}
        inboundTags={inboundTagOptions}
        outboundTags={outboundTagOptions}
        balancerTags={balancerTagOptions}
        onClose={() => setRuleModalOpen(false)}
        onConfirm={onRuleConfirm}
      />
      <PromptModal
        open={importOpen}
        onClose={() => setImportOpen(false)}
        title={t('pages.xray.importRules')}
        okText={t('pages.xray.importRules')}
        type="textarea"
        json
        onConfirm={importRules}
      />

      <Modal
        open={presetSaveOpen}
        title={t('pages.xray.saveRoutingPreset')}
        onCancel={() => setPresetSaveOpen(false)}
        onOk={() => void saveRoutingPreset()}
        confirmLoading={presetBusy}
      >
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Input
            value={presetName}
            onChange={(e) => setPresetName(e.target.value)}
            placeholder={t('pages.xray.routingPresetName')}
            autoFocus
          />
          <Input.TextArea
            value={presetDescription}
            onChange={(e) => setPresetDescription(e.target.value)}
            placeholder={t('pages.xray.routingPresetDescription')}
            autoSize={{ minRows: 3, maxRows: 6 }}
          />
        </Space>
      </Modal>
      <TextModal
        open={exportOpen}
        onClose={() => setExportOpen(false)}
        title={t('pages.xray.exportRules')}
        content={exportContent}
        fileName="routing-rules.json"
        json
      />
    </>
  );
}
