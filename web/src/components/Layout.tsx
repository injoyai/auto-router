import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout as AntLayout, Menu, Button } from 'antd'
import {
  DashboardOutlined,
  AppstoreOutlined,
  SettingOutlined,
  FileTextOutlined,
  LogoutOutlined,
  ThunderboltFilled,
  FireOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'

const { Sider, Content } = AntLayout

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/sources', icon: <AppstoreOutlined />, label: '模型管理' },
  { key: '/queues', icon: <UnorderedListOutlined />, label: '模型队列' },
  { key: '/routing', icon: <SettingOutlined />, label: '路由配置' },
  { key: '/logs', icon: <FileTextOutlined />, label: '日志' },
  { key: '/tokens', icon: <FireOutlined />, label: 'Token 统计' },
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
      <Sider width={248} theme="light" className="aurora-sidebar">
        <div className="aurora-brand">
          <span className="aurora-brand-icon">
            <ThunderboltFilled />
          </span>
          Auto Router
        </div>
        <div className="aurora-brand-sub">AI Gateway</div>
        <Menu
          mode="inline"
          theme="light"
          selectedKeys={[selectedKey]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
        <div style={{ position: 'absolute', bottom: 0, width: '100%', padding: '16px 16px' }}>
          <Button icon={<LogoutOutlined />} block onClick={handleLogout} className="aurora-logout">
            退出登录
          </Button>
        </div>
      </Sider>
      <AntLayout>
        <Content className="aurora-content" style={{ padding: '40px 44px' }}>
          <div className="aurora-fade-in">
            <Outlet />
          </div>
        </Content>
      </AntLayout>
    </AntLayout>
  )
}
