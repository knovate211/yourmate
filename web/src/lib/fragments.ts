// Reusable GraphQL selections. Plain strings: the gateway caches parsed
// documents, so repeating a selection costs nothing at runtime.

export const CAMPUS = `id slug name country city timezone currency emailDomains status`

export const CAMPUS_ADMIN = `${CAMPUS} memberCount website source detailsVerified`

export const ME = `id firstName lastName displayName avatarUrl bio email phone studentIdNo gender
  emailVerified campusEmailVerified campusVerified idVerified isAdmin status joinedAt
  campus { ${CAMPUS} }`

export const SUBJECT = `id code name category campusId`

export const PROVIDER_CARD = `id kind displayName avatarUrl headline rate { amountMinor currency } modes languages services
  status ratingAvg ratingCount sessionsCompleted createdAt isMine
  interviewTypes companies resumeOffer { price { amountMinor currency } turnaroundDays }
  subjects { ${SUBJECT} }
  campus { id name city country timezone }`

export const BOOKING_CARD = `id kind status statusReason startAt endAt durationMinutes mode location meetingLink
  price { amountMinor currency } notes service interviewType reviewed myRole timezone createdAt
  subject { ${SUBJECT} }
  provider { id kind displayName avatarUrl headline }
  student { id displayName avatarUrl campusVerified }`

export const POST = `id type title body imageUrl eventAt location status reviewReason createdAt mine
  author { id displayName avatarUrl }
  campus { id name }`

export const COMMUNITY = `id slug name description kind joinPolicy rules isDefault memberCount postCount createdAt
  myRole requestPending requestDeclined muted canSeePosts canModerate campus { id name }`

export const COMMUNITY_POST = `id body pinned likeCount replyCount likedByMe createdAt mine author { id displayName avatarUrl }`

export const CONVERSATION = `id status isRequestForMe canSend sharesCommunity unreadCount updatedAt
  lastMessage { id body createdAt mine } other { id displayName avatarUrl campusVerified }`

export const RESUME_REVIEW = `id status statusReason targetRole notes fileName price { amountMinor currency } turnaroundDays
  dueAt overdue acceptedAt deliveredAt overallScore rated myRole createdAt
  mentor { id kind displayName avatarUrl headline } student { id displayName avatarUrl }`

export const ROOM = `problem language code codeState whiteboard isOpen opensAt closesAt
  draftScores { criterion score } presence { user { id displayName } joinedAt leftAt }`

export const EXPERIENCE = `id company role year outcome tips anonymous status reviewReason createdAt mine
  rounds { name details } author { id displayName avatarUrl } campus { id name }`

export const QUESTION = `id title body interviewType difficulty companies mine createdAt author { id displayName }`
