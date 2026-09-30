import { lazy, Suspense } from 'react';
import { createBrowserRouter, type RouteObject } from 'react-router';
import { Spin } from 'antd';

import PanelLayout from '@/layouts/PanelLayout';
import { importWithChunkRecovery } from '@/lib/chunk-load-recovery';
import TelemtSubscriptionToggle from '@/pages/telemt/TelemtSubscriptionToggle';

const IndexPage = lazy(() => importWithChunkRecovery(() => import('@/pages/index/IndexPage')));
const InboundsPage = lazy(() =>
  importWithChunkRecovery(() => import('@/pages/inbounds/InboundsPage')),
);
const ClientsPage = lazy(() =>
  importWithChunkRecovery(() => import('@/pages/clients/ClientsPage')),
);
const GroupsPage = lazy(() => importWithChunkRecovery(() => import('@/pages/groups/GroupsPage')));
const NodesPage = lazy(() => importWithChunkRecovery(() => import('@/pages/nodes/NodesPage')));
const HostsPage = lazy(() => importWithChunkRecovery(() => import('@/pages/hosts/HostsPage')));
const FirewallPage = lazy(() =>
  importWithChunkRecovery(() => import('@/pages/firewall/FirewallPage')),
);
const SettingsPage = lazy(() =>
  importWithChunkRecovery(() => import('@/pages/settings/SettingsPage')),
);
const XrayPage = lazy(() => importWithChunkRecovery(() => import('@/pages/xray/XrayPage')));
const SingBoxPage = lazy(() =>
  importWithChunkRecovery(() => import('@/pages/singbox/SingBoxPage')),
);
const ApiDocsPage = lazy(() =>
  importWithChunkRecovery(() => import('@/pages/api-docs/ApiDocsPage')),
);
const TelemtPage = lazy(() => importWithChunkRecovery(() => import('@/pages/telemt/TelemtPage')));

function withSuspense(node: React.ReactNode) {
  return (
    <Suspense
      fallback={
        <div
          style={{
            display: 'flex',
            justifyContent: 'center',
            alignItems: 'center',
            minHeight: '60vh',
          }}
        >
          <Spin size="large" />
        </div>
      }
    >
      {node}
    </Suspense>
  );
}

const routes: RouteObject[] = [
  {
    path: '/',
    element: <PanelLayout />,
    children: [
      { index: true, element: withSuspense(<IndexPage />) },
      { path: 'inbounds', element: withSuspense(<InboundsPage />) },
      { path: 'clients', element: withSuspense(<ClientsPage />) },
      { path: 'groups', element: withSuspense(<GroupsPage />) },
      { path: 'nodes', element: withSuspense(<NodesPage />) },
      { path: 'hosts', element: withSuspense(<HostsPage />) },
      { path: 'firewall', element: withSuspense(<FirewallPage />) },
      { path: 'settings', element: withSuspense(<SettingsPage />) },
      { path: 'xray', element: withSuspense(<XrayPage />) },
      { path: 'singbox', element: withSuspense(<SingBoxPage />) },
      { path: 'outbound', element: withSuspense(<XrayPage />) },
      { path: 'routing', element: withSuspense(<XrayPage />) },
      { path: 'api-docs', element: withSuspense(<ApiDocsPage />) },
      {
        path: 'telemt',
        element: withSuspense(
          <>
            <TelemtPage />
            <TelemtSubscriptionToggle />
          </>,
        ),
      },
    ],
  },
];

function computeBasename() {
  const raw = (typeof window !== 'undefined' && window.X_UI_BASE_PATH) || '/';
  const trimmed = raw.replace(/\/+$/, '');
  return `${trimmed}/panel`;
}

export const router = createBrowserRouter(routes, {
  basename: computeBasename(),
});
