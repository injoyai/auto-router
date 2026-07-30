import type { ThemeConfig } from 'antd'

const theme: ThemeConfig = {
  token: {
    colorPrimary: '#0d9488',
    colorLink: '#0d9488',
    colorBgContainer: '#ffffff',
    colorBgLayout: '#ffffff',
    borderRadius: 8,
    fontFamily: "'Plus Jakarta Sans', sans-serif",
    fontSize: 14,
    colorText: '#171717',
    colorTextSecondary: '#737373',
    colorBorder: '#e5e5e5',
    controlHeight: 38,
    wireframe: false,
  },
  components: {
    Menu: {
      itemHeight: 40,
      iconSize: 16,
      activeBarBorderWidth: 0,
      itemSelectedColor: '#0d9488',
      itemSelectedBg: '#f0fdfa',
    },
    Card: {
      borderRadiusLG: 12,
      headerFontSize: 15,
      paddingLG: 24,
      headerHeight: 52,
    },
    Table: {
      headerBg: '#fafafa',
      headerColor: '#737373',
      rowHoverBg: '#fafafa',
      borderColor: '#f5f5f5',
      cellPaddingBlock: 14,
    },
    Button: {
      primaryShadow: 'none',
      defaultBorderColor: '#e5e5e5',
      defaultColor: '#525252',
      fontWeight: 600,
    },
    Modal: {
      borderRadiusLG: 16,
    },
    Tag: {
      borderRadiusSM: 4,
    },
    Statistic: {
      titleFontSize: 12,
      contentFontSize: 32,
    },
    Input: {
      borderRadius: 8,
      activeBorderColor: '#0d9488',
      activeShadow: '0 0 0 3px rgba(13, 148, 136, 0.08)',
    },
  },
}

export default theme
