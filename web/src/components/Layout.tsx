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
      <Sider width={220} theme="dark" className="aurora-sidebar">
        <div className="aurora-brand">Auto Router</div>
        <div className="aurora-brand-sub">AI Model Gateway</div>
        <Menu
          mode="inline"
          theme="dark"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
        <div style={{ position: 'absolute', bottom: 0, width: '100%', padding: '12px 16px', zIndex: 1 }}>
          <Button icon={<LogoutOutlined />} block onClick={handleLogout} className="aurora-logout">
            退出登录
          </Button>
        </div>
      </Sider>
      <AntLayout>
        <Content className="aurora-content" style={{ padding: 24, position: 'relative', zIndex: 1 }}>
          <div className="aurora-fade-in">
            <Outlet />
          </div>
        </Content>
      </AntLayout>
    </AntLayout>
  )
}
