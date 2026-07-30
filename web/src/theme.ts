import type { ThemeConfig } from 'antd'

const theme: ThemeConfig = {
  token: {
    colorPrimary: '#13c2c2',
    colorLink: '#08979c',
    colorBgContainer: '#ffffff',
    colorBgLayout: '#f0f7f7',
    borderRadius: 8,
    fontFamily: "'DM Sans', sans-serif",
    fontSize: 14,
    colorText: '#1e293b',
    colorTextSecondary: '#64748b',
    colorBorder: '#e2e8f0',
    controlHeight: 38,
    boxShadow: '0 2px 8px rgba(19, 194, 194, 0.08)',
    boxShadowSecondary: '0 4px 16px rgba(19, 194, 194, 0.06)',
  },
  components: {
    Menu: {
      itemHeight: 42,
      iconSize: 16,
      activeBarBorderWidth: 0,
    },
    Card: {
      borderRadiusLG: 12,
      headerFontSize: 16,
      paddingLG: 24,
    },
    Table: {
      headerBg: '#f7fafa',
      headerColor: '#475569',
      rowHoverBg: '#f0fdfa',
      borderColor: '#f0f5f5',
    },
    Button: {
      primaryShadow: '0 2px 8px rgba(19, 194, 194, 0.25)',
    },
    Modal: {
      borderRadiusLG: 16,
    },
    Tag: {
      borderRadiusSM: 6,
    },
    Statistic: {
      titleFontSize: 13,
      contentFontSize: 28,
    },
  },
}

export default theme
