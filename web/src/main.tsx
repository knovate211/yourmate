import { StrictMode, lazy, useEffect, type ComponentType } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes, useLocation } from 'react-router-dom'
import './index.css'
import { AuthProvider } from './lib/auth'
import { CatalogProvider } from './lib/catalog'
import { ToastProvider } from './components/toast'
import { Layout, RequireAdmin, RequireAuth } from './components/Layout'
import { Home } from './pages/public'

// Every page but the home page is its own chunk, loaded when first visited.
// The layout (header, nav) stays mounted while a chunk loads.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function page<M extends Record<K, ComponentType<any>>, K extends keyof M>(load: () => Promise<M>, name: K) {
  return lazy(() => load().then((m) => ({ default: m[name] })))
}
const publicPages = () => import('./pages/public')
const authPages = () => import('./pages/auth')
const providerPages = () => import('./pages/providers')
const bookingPages = () => import('./pages/bookings')
const accountPages = () => import('./pages/account')
const profilePages = () => import('./pages/profile')
const communityPages = () => import('./pages/communities')
const careerPages = () => import('./pages/careers')
const adminPages = () => import('./pages/admin')

const About = page(publicPages, 'About')
const Campuses = page(publicPages, 'Campuses')
const Contact = page(publicPages, 'Contact')
const NotFound = page(publicPages, 'NotFound')
const RequestCampus = page(publicPages, 'RequestCampus')
const ForgotPassword = page(authPages, 'ForgotPassword')
const Login = page(authPages, 'Login')
const Signup = page(authPages, 'Signup')
const VerifyEmail = page(authPages, 'VerifyEmail')
const ProviderDetail = page(providerPages, 'ProviderDetail')
const Providers = page(providerPages, 'Providers')
const BookingDetail = page(bookingPages, 'BookingDetail')
const Bookings = page(bookingPages, 'Bookings')
const Dashboard = page(bookingPages, 'Dashboard')
const Notifications = page(bookingPages, 'Notifications')
const ProviderEditor = page(accountPages, 'ProviderEditor')
const Settings = page(accountPages, 'Settings')
const Verify = page(accountPages, 'Verify')
const Feed = page(() => import('./pages/feed'), 'Feed')
const MyProfile = page(profilePages, 'MyProfile')
const UserProfile = page(profilePages, 'UserProfile')
const CommunityList = page(communityPages, 'CommunityList')
const CommunityPage = page(communityPages, 'CommunityPage')
const CommunityPostPage = page(communityPages, 'CommunityPostPage')
const NewCommunity = page(communityPages, 'NewCommunity')
const Messages = page(() => import('./pages/messages'), 'Messages')
const ResumeReviews = page(careerPages, 'ResumeReviews')
const ResumeReviewDetail = page(careerPages, 'ResumeReviewDetail')
const AdminBookings = page(adminPages, 'AdminBookings')
const AdminCampuses = page(adminPages, 'AdminCampuses')
const AdminInquiries = page(adminPages, 'AdminInquiries')
const AdminLayout = page(adminPages, 'AdminLayout')
const AdminOverview = page(adminPages, 'AdminOverview')
const AdminPosts = page(adminPages, 'AdminPosts')
const AdminProviders = page(adminPages, 'AdminProviders')
const AdminReports = page(adminPages, 'AdminReports')
const AdminSubjects = page(adminPages, 'AdminSubjects')
const AdminUsers = page(adminPages, 'AdminUsers')
const AdminVerifications = page(adminPages, 'AdminVerifications')

function ScrollToTop() {
  const { pathname } = useLocation()
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])
  return null
}

const auth = (el: JSX.Element) => <RequireAuth>{el}</RequireAuth>

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <BrowserRouter>
      <ToastProvider>
        <AuthProvider>
          <CatalogProvider>
          <ScrollToTop />
          <Routes>
            <Route element={<Layout />}>
              <Route index element={<Home />} />
              <Route path="about" element={<About />} />
              <Route path="contact" element={<Contact />} />
              <Route path="campuses" element={<Campuses />} />
              <Route path="campuses/request" element={<RequestCampus />} />
              <Route path="login" element={<Login />} />
              <Route path="signup" element={<Signup />} />
              <Route path="verify-email" element={<VerifyEmail />} />
              <Route path="forgot-password" element={<ForgotPassword />} />

              <Route path="tutors" element={<Providers kind="TUTOR" key="tutors" />} />
              <Route path="tutors/:id" element={<ProviderDetail kind="TUTOR" />} />
              <Route path="guides" element={<Providers kind="GUIDE" key="guides" />} />
              <Route path="guides/:id" element={<ProviderDetail kind="GUIDE" />} />
              <Route path="mentors" element={<Providers kind="MENTOR" key="mentors" />} />
              <Route path="mentors/:id" element={<ProviderDetail kind="MENTOR" />} />
              <Route path="resume-reviews" element={auth(<ResumeReviews />)} />
              <Route path="resume-reviews/:id" element={auth(<ResumeReviewDetail />)} />
              <Route path="feed" element={<Feed />} />
              <Route path="communities" element={<CommunityList />} />
              <Route path="communities/new" element={auth(<NewCommunity />)} />
              <Route path="communities/:slug" element={<CommunityPage />} />
              <Route path="communities/:slug/posts/:postId" element={auth(<CommunityPostPage />)} />

              <Route path="dashboard" element={auth(<Dashboard />)} />
              <Route path="dashboard/profile" element={auth(<ProviderEditor />)} />
              <Route path="bookings" element={auth(<Bookings />)} />
              <Route path="bookings/:id" element={auth(<BookingDetail />)} />
              <Route path="notifications" element={auth(<Notifications />)} />
              <Route path="messages" element={auth(<Messages />)} />
              <Route path="messages/:id" element={auth(<Messages />)} />
              <Route path="verify" element={auth(<Verify />)} />
              <Route path="settings" element={auth(<Settings />)} />
              <Route path="profile" element={auth(<MyProfile />)} />
              <Route path="users/:id" element={auth(<UserProfile />)} />

              <Route path="admin" element={<RequireAdmin><AdminLayout /></RequireAdmin>}>
                <Route index element={<AdminOverview />} />
                <Route path="verifications" element={<AdminVerifications />} />
                <Route path="campuses" element={<AdminCampuses />} />
                <Route path="users" element={<AdminUsers />} />
                <Route path="providers" element={<AdminProviders />} />
                <Route path="posts" element={<AdminPosts />} />
                <Route path="reports" element={<AdminReports />} />
                <Route path="bookings" element={<AdminBookings />} />
                <Route path="inquiries" element={<AdminInquiries />} />
                <Route path="subjects" element={<AdminSubjects />} />
              </Route>
              <Route path="*" element={<NotFound />} />
            </Route>
          </Routes>
          </CatalogProvider>
        </AuthProvider>
      </ToastProvider>
    </BrowserRouter>
  </StrictMode>,
)
