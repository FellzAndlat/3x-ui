import { Card, ConfigProvider, FloatButton, Layout, Tabs } from 'antd';
import { useNavigate } from 'react-router';

import AdBlockTab from '@/pages/adblock/AdBlockTab';
import AppSidebar from '@/layouts/AppSidebar';
import GatewayModeControl from '@/pages/settings/GatewayModeControl';
import { useTheme } from '@/hooks/useTheme';
import './SingBoxPage.css';

type UtilitySection = 'adblock' | 'gateway';

type Props = {
  section: UtilitySection;
};

const tabs = [
  { key: 'basic', label: 'Основные' },
  { key: 'dns', label: 'DNS' },
  { key: 'routing', label: 'Маршрутизация' },
  { key: 'adblock', label: 'AdBlock' },
  { key: 'outbound', label: 'Исходящие' },
  { key: 'gateway', label: 'Режим шлюза' },
];

function scrollTarget() {
  return document.getElementById('content-layout') || window;
}

export default function SingBoxUtilityPage({ section }: Props) {
  const navigate = useNavigate();
  const { antdThemeConfig, isDark, isUltra } = useTheme();

  return (
    <ConfigProvider theme={antdThemeConfig}>
      <Layout
        className={`singbox-page ${isDark ? 'is-dark ' : ''}${isUltra ? 'is-ultra' : ''}`.trim()}
      >
        <AppSidebar />
        <Layout className="content-shell">
          <Layout.Content id="content-layout" className="content-area">
            <FloatButton.BackTop target={scrollTarget} visibilityHeight={200} />
            <Tabs
              activeKey={section}
              className="singbox-main-tabs"
              onChange={(key) => navigate(`/singbox#${key}`)}
              items={tabs.map((tab) => ({
                ...tab,
                children:
                  tab.key === section ? (
                    section === 'gateway' ? (
                      <Card hoverable>
                        <GatewayModeControl />
                      </Card>
                    ) : (
                      <AdBlockTab />
                    )
                  ) : null,
              }))}
            />
          </Layout.Content>
        </Layout>
      </Layout>
    </ConfigProvider>
  );
}
