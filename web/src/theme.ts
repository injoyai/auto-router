import type { ThemeConfig } from 'antd'

const theme: ThemeConfig = {
  token: {
    colorPrimary: '#3a6b4d',
    colorLink: '#3a6b4d',
    colorBgContainer: '#ffffff',
    colorBgLayout: '#faf8f3',
    borderRadius: 10,
    fontFamily: "'DM Sans', sans-serif",
    fontSize: 14,
    colorText: '#1f1a12',
    colorTextSecondary: '#8a7f66',
    colorBorder: '#e2dccd',
    controlHeight: 40,
    wireframe: false,
  },
  components: {
    Menu: {
      itemHeight: 42,
      iconSize: 16,
      activeBarBorderWidth: 0,
      itemSelectedColor: '#3a6b4d',
      itemSelectedBg: 'rgba(255, 255, 255, 0.75)',
      itemBorderRadius: 10,
      itemMarginInline: 10,
    },
    Card: {
      borderRadiusLG: 22,
      headerFontSize: 15,
      paddingLG: 24,
      headerHeight: 56,
    },
    Table: {
      headerBg: 'rgba(250, 248, 243, 0.5)',
      headerColor: '#a89e85',
      rowHoverBg: '#e8f0ea',
      borderColor: '#f0ece2',
      cellPaddingBlock: 14,
    },
    Button: {
      primaryShadow: 'none',
      defaultBorderColor: '#e2dccd',
      defaultColor: '#6a604c',
      fontWeight: 600,
      borderRadius: 10,
    },
    Modal: {
      borderRadiusLG: 28,
    },
    Tag: {
      borderRadiusSM: 7,
    },
    Statistic: {
      titleFontSize: 13,
      contentFontSize: 32,
    },
    Input: {
      borderRadius: 10,
      activeBorderColor: '#3a6b4d',
      activeShadow: '0 0 0 4px rgba(58, 107, 77, 0.12)',
    },
    Select: {
      borderRadius: 10,
    },
  },
}

export default theme
