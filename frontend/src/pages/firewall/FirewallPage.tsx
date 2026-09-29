import { ConfigProvider, Layout, Space, Typography } from 'antd';
import { SafetyOutlined } from '@ant-design/icons';
import { useTranslation } from 'react-i18next';

import AppSidebar from '@/layouts/AppSidebar';
import { useTheme } from '@/hooks/useTheme';
import { FirewallManager } from '@/pages/xray/FirewallModal';

export default function FirewallPage() {
  const { i18n } = useTranslation();
  const { antdThemeConfig } = useTheme();
  const ru = (i18n.resolvedLanguage || i18n.language || '').toLowerCase().startsWith('ru');
  const title = ru ? 'Файрволл' : 'Firewall';
  const subtitle = ru
    ? 'Управление системным файрволлом, автоматической синхронизацией и ручными правилами портов.'
    : 'Manage the system firewall, automatic synchronization, and manual port rules.';

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout className="page-layout">
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <div style={{ width: '100%', maxWidth: 1100, margin: '0 auto' }}>
              <Space direction="vertical" size={4} style={{ marginBottom: 20 }}>
                <Typography.Title level={2} style={{ margin: 0 }}>
                  <Space>
                    <SafetyOutlined />
                    {title}
                  </Space>
                </Typography.Title>
                <Typography.Text type="secondary">{subtitle}</Typography.Text>
              </Space>
              <FirewallManager />
            </div>
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
