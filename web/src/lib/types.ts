// Shapes of the GraphQL data the app reads. Kept by hand next to the
// fragments in fragments.ts; fields are optional where a query may omit them.

export type ProviderKind = 'TUTOR' | 'GUIDE' | 'MENTOR'
export type SessionMode = 'ONLINE' | 'IN_PERSON'
export type ProviderStatus = 'DRAFT' | 'ACTIVE' | 'PAUSED' | 'SUSPENDED'
export type BookingStatus = 'REQUESTED' | 'ACCEPTED' | 'DECLINED' | 'CANCELLED' | 'COMPLETED' | 'EXPIRED'
export type BookingRole = 'STUDENT' | 'PROVIDER' | 'ANY'
export type VerificationStatus = 'NONE' | 'PENDING' | 'APPROVED' | 'REJECTED'
export type PostType = 'EVENT' | 'NOTICE' | 'PLACE' | 'TIP'
export type PostStatus = 'PENDING' | 'PUBLISHED' | 'REJECTED'
export type CampusStatus = 'ACTIVE' | 'PENDING' | 'REJECTED'

export interface Money {
  amountMinor: number
  currency: string
}

export interface Campus {
  id: string
  slug: string
  name: string
  country: string
  city: string
  timezone: string
  currency: string
  emailDomains: string[]
  status: CampusStatus
  memberCount?: number
  website?: string | null
  source?: 'SEED' | 'DIRECTORY' | 'REQUEST'
  detailsVerified?: boolean
}

export interface CountryCount {
  country: string
  campuses: number
}

export interface User {
  id: string
  firstName: string
  displayName: string
  avatarUrl: string | null
  bio: string
  campusVerified: boolean
  idVerified: boolean
  isAdmin: boolean
  joinedAt: string
  campus?: Campus | null
  lastName?: string | null
  email?: string | null
  phone?: string | null
  studentIdNo?: string | null
  gender?: string | null
  emailVerified?: boolean | null
  campusEmailVerified?: boolean | null
  status?: 'ACTIVE' | 'SUSPENDED' | null
}

export interface Subject {
  id: string
  code: string
  name: string
  category: string
  campusId: string | null
}

export interface AvailabilityWindow {
  weekday: number
  startMinute: number
  endMinute: number
}

export interface Availability {
  timezone: string
  windows: AvailabilityWindow[]
}

export interface Review {
  id: string
  rating: number
  comment: string
  hidden: boolean
  createdAt: string
  reviewer: Pick<User, 'id' | 'displayName' | 'avatarUrl'>
}

export interface Provider {
  id: string
  kind: ProviderKind
  displayName: string
  avatarUrl: string | null
  headline: string
  bio: string
  rate: Money
  modes: SessionMode[]
  languages: string[]
  services: string[]
  subjects: Subject[]
  status: ProviderStatus
  ratingAvg: number
  ratingCount: number
  sessionsCompleted: number
  createdAt: string
  isMine: boolean
  interviewTypes: string[]
  companies: string[]
  resumeOffer: { price: Money; turnaroundDays: number } | null
  resumeReviewsDelivered?: number
  campus?: Campus | null
  user?: Pick<User, 'id' | 'displayName' | 'campusVerified' | 'idVerified' | 'joinedAt' | 'bio'>
  availability?: Availability
  reviews?: { total: number; items: Review[] }
}

export interface Message {
  id: string
  body: string
  createdAt: string
  mine: boolean
  sender: Pick<User, 'id' | 'displayName' | 'avatarUrl'>
}

export interface Booking {
  id: string
  kind: ProviderKind
  status: BookingStatus
  statusReason: string
  startAt: string
  endAt: string
  durationMinutes: number
  mode: SessionMode
  location: string
  meetingLink: string | null
  price: Money
  notes: string
  service: string | null
  interviewType: string | null
  feedback?: InterviewFeedback | null
  reviewed: boolean
  myRole: BookingRole
  timezone: string
  createdAt: string
  subject: Subject | null
  provider: Pick<Provider, 'id' | 'kind' | 'displayName' | 'avatarUrl' | 'headline'>
  student: Pick<User, 'id' | 'displayName' | 'avatarUrl' | 'campusVerified'>
  providerUser?: Pick<User, 'id'>
  cancelledBy?: Pick<User, 'id' | 'displayName'> | null
  contact?: { name: string; email: string; phone: string } | null
  messages?: Message[]
}

export interface Notification {
  id: string
  type: string
  title: string
  body: string
  link: string
  read: boolean
  createdAt: string
}

export interface Verification {
  id: string | null
  status: VerificationStatus
  reason: string
  createdAt: string | null
  reviewedAt: string | null
  documentUrl?: string | null
  user?: User | null
}

export interface Post {
  id: string
  type: PostType
  title: string
  body: string
  imageUrl: string | null
  eventAt: string | null
  location: string
  status: PostStatus
  reviewReason: string
  createdAt: string
  mine: boolean
  author: Pick<User, 'id' | 'displayName' | 'avatarUrl'>
  campus?: Pick<Campus, 'id' | 'name'> | null
}

export interface Page<T> {
  items: T[]
  total: number
}

export type CommunityKind = 'GENERAL' | 'COURSE' | 'HOSTEL' | 'CLUB' | 'INTEREST' | 'EXAM'
export type CommunityRole = 'NONE' | 'MEMBER' | 'MODERATOR' | 'OWNER'

export interface Community {
  id: string
  slug: string
  name: string
  description: string
  kind: CommunityKind
  joinPolicy: 'OPEN' | 'APPROVAL'
  rules: string
  isDefault: boolean
  memberCount: number
  postCount: number
  createdAt: string
  myRole: CommunityRole
  requestPending: boolean
  requestDeclined: boolean
  muted: boolean
  canSeePosts: boolean
  canModerate: boolean
  campus: Pick<Campus, 'id' | 'name'> | null
  owner?: Pick<User, 'id' | 'displayName'> | null
}

export interface CommunityPost {
  id: string
  body: string
  pinned: boolean
  likeCount: number
  replyCount: number
  likedByMe: boolean
  createdAt: string
  mine: boolean
  author: Pick<User, 'id' | 'displayName' | 'avatarUrl'>
}

export interface CommunityReply {
  id: string
  body: string
  createdAt: string
  mine: boolean
  author: Pick<User, 'id' | 'displayName' | 'avatarUrl'>
}

export interface DirectMessage {
  id: string
  body: string
  createdAt: string
  mine: boolean
}

export interface Conversation {
  id: string
  status: 'REQUEST' | 'ACTIVE'
  isRequestForMe: boolean
  canSend: boolean
  sharesCommunity: boolean
  unreadCount: number
  updatedAt: string
  lastMessage: DirectMessage | null
  other: Pick<User, 'id' | 'displayName' | 'avatarUrl' | 'campusVerified'>
}

export interface InterviewFeedback {
  scores: { criterion: string; score: number }[]
  verdict: 'READY' | 'ALMOST' | 'NOT_YET'
  strengths: string
  improvements: string
  resources: string
  updatedAt: string
}

export type ResumeReviewStatus = 'REQUESTED' | 'ACCEPTED' | 'DELIVERED' | 'DECLINED' | 'CANCELLED' | 'EXPIRED'

export interface ResumeReview {
  id: string
  status: ResumeReviewStatus
  statusReason: string
  targetRole: string
  notes: string
  fileName: string
  fileUrl: string | null
  feedbackFileUrl: string | null
  price: Money
  turnaroundDays: number
  dueAt: string | null
  overdue: boolean
  acceptedAt: string | null
  deliveredAt: string | null
  overallScore: number | null
  summary: string
  sections: { section: string; rating: number; comment: string }[]
  rated: boolean
  myRole: 'STUDENT' | 'REVIEWER' | 'ANY'
  createdAt: string
  mentor: Pick<Provider, 'id' | 'kind' | 'displayName' | 'avatarUrl' | 'headline'>
  student: Pick<User, 'id' | 'displayName' | 'avatarUrl'>
  cancelledBy?: Pick<User, 'id' | 'displayName'> | null
}
