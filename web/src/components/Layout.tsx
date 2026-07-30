import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout as AntLayout, Menu, Button } from 'antd'
import {
  DashboardOutlined,
  AppstoreOutlined,
  SettingOutlined,
  FileTextOutlined,
  LogoutOutlined,
} from '@ant-design/icons'

const { Sider, Content } = AntLayout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/sources', icon: <AppstoreOutlined />, label: '模型管理' },
  { key: '/routing', icon: <SettingOutlined />, label: '路由配置' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
]

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = location.pathname === '/' ? '/' : '/' + location.pathname.split('/').filter(Boolean)[0]

  const handleLogout = () => {
    localStorage.removeItem('admin_jwt')
    navigate('/login')
  }

  return (
    <AntLayout style={{ minHeight: '100vh' }}>
      <Sider width={240} theme="light" className="mono-sidebar">
        <div className="mono-brand">Auto Router</div>
        <div className="mono-brand-sub">AI Gateway</div>
        <Menu
          mode="inline"
          theme="light"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
        <div style={{ position: 'absolute', bottom: 0, width: '100%', padding: '16px 16px' }}>
          <Button icon={<LogoutOutlined />} block onClick={handleLogout} className="mono-logout">
            退出登录
          </Button>
        </div>
      </Sider>
      <AntLayout>
        <Content className="mono-content" style={{ padding: 32 }}>
          <div className="mono-fade-in">
            <Outlet />
          </div>
        </Content>
      </AntLayout>
    </AntLayout>
  )
}
