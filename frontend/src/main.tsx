import { createRoot } from 'react-dom/client';
import { RouterProvider } from 'react-router/dom';
import { ConfigProvider, message } from 'antd';
import 'antd/dist/reset.css';
import '@/styles/utils.css';
import '@/styles/page-shell.css';
import '@/styles/page-cards.css';
import '@/styles/material-ui.css';
import '@/styles/sidebar-brand-alignment.css';

import { setupHttp } from '@/api/http-init';
import { readyI18n } from '@/i18n/react';
import { ThemeProvider, useTheme } from '@/hooks/useTheme';
import { QueryProvider } from '@/api/QueryProvider';
import { installChunkLoadRecovery } from '@/lib/chunk-load-recovery';
import { router } from '@/routes';

installChunkLoadRecovery();
setupHttp();

const messageContainer = document.getElementById('message');
if (messageContainer) {
  message.config({ getContainer: () => messageContainer });
}

function AppProviders() {
  const { antdThemeConfig } = useTheme();
  return (
    <ConfigProvider theme={antdThemeConfig}>
      <QueryProvider>
        <RouterProvider router={router} />
      </QueryProvider>
    </ConfigProvider>
  );
}

readyI18n().then(() => {
  const root = document.getElementById('app');
  if (root) {
    createRoot(root).render(
      <ThemeProvider>
        <AppProviders />
      </ThemeProvider>,
    );
  }
});
