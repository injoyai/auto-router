import { Outlet, useNavigate, useLocation } from 'react-router-dom'
import { Layout as AntLayout, Menu } from 'antd'
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

const LOGOUT_KEY = '__logout__'

const menuItems = [
  { key: '/', icon: <DashboardOutlined />, label: '仪表盘' },
  { key: '/sources', icon: <AppstoreOutlined />, label: '模型管理' },
  { key: '/queues', icon: <UnorderedListOutlined />, label: '模型队列' },
  { key: '/routing', icon: <SettingOutlined />, label: '路由配置' },
  { key: '/logs', icon: <FileTextOutlined />, label: '请求日志' },
  { key: '/tokens', icon: <FireOutlined />, label: 'Token 统计' },
  { type: 'divider' as const, className: 'aurora-menu-divider' },
  { key: LOGOUT_KEY, icon: <LogoutOutlined />, label: '退出登录', danger: true, className: 'aurora-menu-logout' },
]

export default function Layout() {
  const navigate = useNavigate()
  const location = useLocation()

  const selectedKey = location.pathname === '/' ? '/' : '/' + location.pathname.split('/').filter(Boolean)[0]

  const handleLogout = () => {
    localStorage.removeItem('admin_jwt')
    navigate('/login')
  }

  const onMenuClick = ({ key }: { key: string }) => {
    if (key === LOGOUT_KEY) {
      handleLogout()
      return
    }
    navigate(key)
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
          onClick={onMenuClick}
        />
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
