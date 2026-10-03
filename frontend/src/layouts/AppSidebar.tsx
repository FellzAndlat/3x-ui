import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { ComponentType, CSSProperties } from 'react';
import { useLocation, useNavigate } from 'react-router';
import { useTranslation } from 'react-i18next';
import { Drawer, Layout, Menu } from 'antd';
import type { MenuProps } from 'antd';
import {
  ApartmentOutlined,
  CloseOutlined,
  CloudServerOutlined,
  ClusterOutlined,
  CodeOutlined,
  DashboardOutlined,
  DatabaseOutlined,
  DiscordOutlined,
  ExportOutlined,
  GlobalOutlined,
  ImportOutlined,
  LogoutOutlined,
  MailOutlined,
  MenuOutlined,
  MessageOutlined,
  PushpinFilled,
  PushpinOutlined,
  SafetyOutlined,
  SettingOutlined,
  SwapOutlined,
  TagsOutlined,
  TeamOutlined,
  ToolOutlined,
} from '@ant-design/icons';
import { HttpUtil } from '@/utils';
import { useTheme } from '@/hooks/useTheme';
import { useAllSettings } from '@/api/queries/useAllSettings';
import './AppSidebar.css';

const LOGOUT_KEY = '__logout__';

function BrandMark() {
  return (
    <span className="brand-mark" aria-hidden="true">
      <span className="brand-mark-default">
        <svg viewBox="0 0 64 64" role="presentation">
          <path className="brand-mark-default-frame" d="M20 6h24l14 14v24L44 58H20L6 44V20L20 6Z" />
          <path className="brand-mark-default-core" d="M20 20h24M17 32h30M20 44h24" />
          <path className="brand-mark-default-x" d="m24 24 16 16M40 24 24 40" />
          <path className="brand-mark-default-node" d="M6 22h7M51 22h7M6 42h7M51 42h7" />
          <circle className="brand-mark-default-dot" cx="32" cy="12" r="2" />
          <circle
            className="brand-mark-default-dot brand-mark-default-dot-pink"
            cx="32"
            cy="52"
            r="2"
          />
        </svg>
      </span>
    </span>
  );
}
const RAIL_WIDTH = 72;
const SIDER_WIDTH = 220;
const SIDEBAR_PINNED_KEY = 'sidebar-pinned';
let hoveredAcrossRemounts = false;

type IconName =
  | 'dashboard'
  | 'inbound'
  | 'team'
  | 'groups'
  | 'setting'
  | 'cluster'
  | 'hosts'
  | 'firewall'
  | 'logout'
  | 'outbound'
  | 'routing'
  | 'telemt'
  | 'tool';
const iconByName: Record<IconName, ComponentType> = {
  dashboard: DashboardOutlined,
  inbound: ImportOutlined,
  team: TeamOutlined,
  groups: TagsOutlined,
  setting: SettingOutlined,
  cluster: ClusterOutlined,
  hosts: GlobalOutlined,
  firewall: SafetyOutlined,
  logout: LogoutOutlined,
  outbound: ExportOutlined,
  routing: SwapOutlined,
  telemt: MessageOutlined,
  tool: ToolOutlined,
};
function readSidebarPinned() {
  try {
    return localStorage.getItem(SIDEBAR_PINNED_KEY) === 'true';
  } catch {
    return false;
  }
}
function saveSidebarPinned(pinned: boolean) {
  try {
    localStorage.setItem(SIDEBAR_PINNED_KEY, String(pinned));
  } catch {}
}

function AppSidebar() {
  const { t, i18n } = useTranslation();
  const { mode } = useTheme();
  const navigate = useNavigate();
  const { pathname, hash } = useLocation();
  const { allSetting } = useAllSettings();
  const showSubFormats = !!(allSetting.subJsonEnable || allSetting.subClashEnable);
  const showSubBalancers = !!allSetting.subJsonEnable;
  const isXray = allSetting.coreType === 'xray';
  const isSingBox = allSetting.coreType === 'sing-box';
  const firewallTitle = (i18n.resolvedLanguage || i18n.language || '')
    .toLowerCase()
    .startsWith('ru')
    ? 'Файрволл'
    : 'Firewall';
  const [hovered, setHovered] = useState(() => hoveredAcrossRemounts);
  const [pinned, setPinned] = useState(readSidebarPinned);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [drawerMounted, setDrawerMounted] = useState(false);
  const resetDrawerSideEffects = useCallback(() => {
    document.documentElement.style.removeProperty('overflow');
    document.body.style.removeProperty('overflow');
    document.body.style.removeProperty('touch-action');
  }, []);
  const openDrawer = useCallback(() => {
    setDrawerMounted(true);
    setDrawerOpen(true);
  }, []);
  const closeDrawer = useCallback(() => {
    setDrawerOpen(false);
    resetDrawerSideEffects();
  }, [resetDrawerSideEffects]);
  const railCollapsed = !hovered && !pinned;
  const railStyle = useMemo(
    () => ({ '--sider-rail': `${pinned ? SIDER_WIDTH : RAIL_WIDTH}px` }) as CSSProperties,
    [pinned],
  );
  const rootRef = useRef<HTMLDivElement>(null);
  const updateHovered = useCallback((value: boolean) => {
    hoveredAcrossRemounts = value;
    setHovered(value);
  }, []);
  const togglePinned = useCallback(() => {
    const next = !pinned;
    saveSidebarPinned(next);
    setPinned(next);
  }, [pinned]);
  useEffect(() => {
    const timer = window.setTimeout(() => {
      const el = rootRef.current;
      if (el) updateHovered(el.matches(':hover'));
    }, 150);
    return () => window.clearTimeout(timer);
  }, [updateHovered]);
  const currentTheme: 'light' | 'dark' = mode === 'light' || mode === 'colorful' ? 'light' : 'dark';

  const xrayChildren = useMemo<NonNullable<MenuProps['items']>>(
    () => [
      { key: '/xray#basic', icon: <SettingOutlined />, label: t('pages.xray.basicTemplate') },
      { key: '/settings#gateway', icon: <ApartmentOutlined />, label: 'Режим шлюза' },
      { key: '/xray#balancer', icon: <ClusterOutlined />, label: t('pages.xray.Balancers') },
      { key: '/xray#dns', icon: <DatabaseOutlined />, label: 'DNS' },
      { key: '/xray#advanced', icon: <CodeOutlined />, label: t('pages.xray.advancedTemplate') },
    ],
    [t],
  );

  const singBoxChildren = useMemo<NonNullable<MenuProps['items']>>(
    () => [
      {
        key: '/singbox#basic',
        icon: <SettingOutlined />,
        label: t('pages.singBox.sections.basic'),
      },
      {
        key: '/singbox#dns',
        icon: <DatabaseOutlined />,
        label: t('pages.singBox.sections.dns'),
      },
      {
        key: '/singbox#routing',
        icon: <SwapOutlined />,
        label: t('pages.singBox.sections.route'),
      },
      {
        key: '/singbox#adblock',
        icon: <SafetyOutlined />,
        label: 'AdBlock',
      },
      {
        key: '/singbox#outbound',
        icon: <ExportOutlined />,
        label: t('pages.singBox.sections.outbounds'),
      },
      {
        key: '/singbox#gateway',
        icon: <ApartmentOutlined />,
        label: 'Режим шлюза',
      },
      {
        key: '/singbox#endpoints',
        icon: <ClusterOutlined />,
        label: 'Endpoints',
      },
      {
        key: '/singbox#certificates',
        icon: <SafetyOutlined />,
        label: 'Сертификаты',
      },
      {
        key: '/singbox#network',
        icon: <GlobalOutlined />,
        label: 'Сеть и HTTP-клиенты',
      },
    ],
    [t],
  );

  const settingsChildren = useMemo<NonNullable<MenuProps['items']>>(() => {
    const children: NonNullable<MenuProps['items']> = [
      {
        key: '/settings#general',
        icon: <SettingOutlined />,
        label: t('pages.settings.panelSettings'),
      },
      {
        key: '/settings#security',
        icon: <SafetyOutlined />,
        label: t('security'),
      },
      {
        key: '/settings#telegram',
        icon: <MessageOutlined />,
        label: t('pages.settings.TGBotSettings'),
      },
      { key: '/settings#email', icon: <MailOutlined />, label: t('pages.settings.emailSettings') },
      {
        key: '/settings#discord',
        icon: <DiscordOutlined />,
        label: t('pages.settings.discordSettings'),
      },
      {
        key: '/settings#subscription',
        icon: <CloudServerOutlined />,
        label: t('pages.settings.subSettings'),
      },
    ];
    if (showSubFormats)
      children.push({
        key: '/settings#subscription-formats',
        icon: <CodeOutlined />,
        label: t('menu.subFormats'),
      });
    if (showSubBalancers)
      children.push({
        key: '/settings#subscription-balancers',
        icon: <ApartmentOutlined />,
        label: t('pages.settings.subBalancers.menu'),
      });
    return children;
  }, [t, showSubFormats, showSubBalancers]);

  const tabs = useMemo<
    { key: string; icon: IconName; title: string; children?: MenuProps['items'] }[]
  >(() => {
    const base = [
      { key: '/', icon: 'dashboard' as IconName, title: t('menu.dashboard') },
      { key: '/inbounds', icon: 'inbound' as IconName, title: t('menu.inbounds') },
      { key: '/clients', icon: 'team' as IconName, title: t('menu.clients') },
      { key: '/groups', icon: 'groups' as IconName, title: t('menu.groups') },
      { key: '/nodes', icon: 'cluster' as IconName, title: t('menu.nodes') },
      { key: '/hosts', icon: 'hosts' as IconName, title: t('menu.hosts') },
      { key: '/firewall', icon: 'firewall' as IconName, title: firewallTitle },
      { key: '/outbound', icon: 'outbound' as IconName, title: t('menu.outbounds') },
      { key: '/routing', icon: 'routing' as IconName, title: t('menu.routing') },
      { key: '/telemt', icon: 'telemt' as IconName, title: t('menu.telemt') },
      { key: '/settings', icon: 'setting' as IconName, title: t('menu.settings') },
      ...(isXray
        ? [
            {
              key: '/xray',
              icon: 'tool' as IconName,
              title: t('menu.xray'),
              children: xrayChildren,
            },
          ]
        : []),
      ...(isSingBox
        ? [
            {
              key: '/singbox',
              icon: 'tool' as IconName,
              title: t('menu.singBox'),
              children: singBoxChildren,
            },
          ]
        : []),
      { key: LOGOUT_KEY, icon: 'logout' as IconName, title: t('logout') },
    ];
    return base;
  }, [t, firewallTitle, isXray, isSingBox, xrayChildren, singBoxChildren]);

  const navItems = useMemo(() => tabs.filter((tab) => tab.icon !== 'logout'), [tabs]);
  const utilItems = useMemo(() => tabs.filter((tab) => tab.icon === 'logout'), [tabs]);

  const settingsActive = pathname === '/settings';
  const xrayActive = pathname === '/xray';
  const singBoxActive = pathname === '/singbox';
  const gatewayActive = settingsActive && hash === '#gateway';
  const selectedKey = settingsActive
    ? `/settings${hash || '#general'}`
    : xrayActive
      ? `/xray${hash || '#basic'}`
      : singBoxActive
        ? `/singbox${hash || '#basic'}`
        : pathname === ''
          ? '/'
          : pathname;
  const gatewaySubmenu = isXray ? '/xray' : '/settings';
  const openSubmenu = gatewayActive
    ? gatewaySubmenu
    : settingsActive
      ? '/settings'
      : xrayActive
        ? '/xray'
        : singBoxActive
          ? '/singbox'
          : null;
  const [openKeys, setOpenKeys] = useState<string[]>(() => (openSubmenu ? [openSubmenu] : []));
  const visibleOpenKeys = useMemo(() => {
    let keys = openKeys;
    if (openSubmenu && !keys.includes(openSubmenu)) keys = [...keys, openSubmenu];
    return keys;
  }, [openKeys, openSubmenu]);
  const toMenuItems = useCallback(
    (items: typeof tabs): MenuProps['items'] =>
      items.map((tab) => {
        const Icon = iconByName[tab.icon];
        if (tab.key === '/settings')
          return { key: tab.key, icon: <Icon />, label: tab.title, children: settingsChildren };
        return {
          key: tab.key,
          icon: <Icon />,
          label: tab.title,
          title: '',
          children: tab.children,
        };
      }),
    [settingsChildren],
  );
  const openLink = useCallback(
    async (key: string) => {
      if (key === LOGOUT_KEY) {
        await HttpUtil.post('/logout');
        window.location.href = window.X_UI_BASE_PATH || '/';
        return;
      }
      navigate(key);
    },
    [navigate],
  );
  const onMenuClick = useCallback<NonNullable<MenuProps['onClick']>>(
    ({ key }) => {
      void openLink(String(key));
    },
    [openLink],
  );
  return (
    <div
      ref={rootRef}
      className={`ant-sidebar${pinned ? ' sidebar-pinned' : ''}`}
      style={railStyle}
      onMouseEnter={() => updateHovered(true)}
      onMouseLeave={() => updateHovered(false)}
    >
      <Layout.Sider
        theme={currentTheme}
        width={SIDER_WIDTH}
        collapsedWidth={RAIL_WIDTH}
        collapsed={railCollapsed}
      >
        <div className="sider-brand">
          <div
            className={`brand-block sider-brand-content ${railCollapsed ? 'brand-is-compact' : 'brand-is-expanded'}`}
            style={{ height: 72 }}
          >
            <span className="brand-text" aria-label={railCollapsed ? '3X' : '3X-UI'}>
              <span className="brand-text-full">3X-UI</span>
              <span className="brand-text-compact">3X</span>
            </span>
          </div>
          <div className="brand-actions">
            {!railCollapsed && (
              <button
                type="button"
                className="sidebar-pin"
                aria-label={t('menu.pinSidebar')}
                aria-pressed={pinned}
                title={t(pinned ? 'menu.unpinSidebar' : 'menu.pinSidebar')}
                onClick={togglePinned}
              >
                {pinned ? <PushpinFilled /> : <PushpinOutlined />}
              </button>
            )}
          </div>
        </div>
        <Menu
          theme={currentTheme}
          mode="inline"
          selectedKeys={[selectedKey]}
          openKeys={railCollapsed ? undefined : visibleOpenKeys}
          onOpenChange={(keys) => setOpenKeys(keys as string[])}
          className="sider-nav"
          items={toMenuItems(navItems)}
          onClick={onMenuClick}
        />
        <Menu
          theme={currentTheme}
          mode="inline"
          selectedKeys={[selectedKey]}
          className="sider-utility"
          items={toMenuItems(utilItems)}
          onClick={onMenuClick}
        />
      </Layout.Sider>
      {drawerMounted && (
        <Drawer
          placement="left"
          closable={false}
          open={drawerOpen}
          rootClassName={currentTheme}
          size="min(82vw, 320px)"
          mask={{ enabled: true, blur: false }}
          styles={{
            wrapper: { padding: 0 },
            body: { padding: 0, display: 'flex', flexDirection: 'column', height: '100%' },
            header: { display: 'none' },
            mask: { backdropFilter: 'none', WebkitBackdropFilter: 'none', filter: 'none' },
          }}
          afterOpenChange={(open) => {
            if (!open) {
              resetDrawerSideEffects();
              setDrawerMounted(false);
            }
          }}
          onClose={closeDrawer}
        >
          <div className="drawer-header">
            <div className="drawer-header-brand-mark">
              <BrandMark />
            </div>
            <span className="drawer-brand">3X-UI</span>
            <div className="drawer-header-actions">
              <button
                className="drawer-close"
                type="button"
                aria-label={t('close')}
                onClick={closeDrawer}
              >
                <CloseOutlined />
              </button>
            </div>
          </div>
          <Menu
            theme={currentTheme}
            mode="inline"
            selectedKeys={[selectedKey]}
            openKeys={visibleOpenKeys}
            onOpenChange={(keys) => setOpenKeys(keys as string[])}
            className="drawer-menu drawer-nav"
            items={toMenuItems(navItems)}
            onClick={(info) => {
              onMenuClick(info);
              closeDrawer();
            }}
          />
          <Menu
            theme={currentTheme}
            mode="inline"
            selectedKeys={[selectedKey]}
            className="drawer-menu drawer-utility"
            items={toMenuItems(utilItems)}
            onClick={(info) => {
              onMenuClick(info);
              closeDrawer();
            }}
          />
        </Drawer>
      )}
      {!drawerMounted && (
        <button
          className="drawer-handle"
          type="button"
          aria-label={t('menu.openMenu')}
          onClick={openDrawer}
        >
          <MenuOutlined />
        </button>
      )}
    </div>
  );
}
export default memo(AppSidebar);
