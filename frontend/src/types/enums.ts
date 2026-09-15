// 需求状态（与后端 backend/internal/constants/requirement_status.go 对齐）
export enum RequirementStatus {
  Draft = 'draft',
  Open = 'open',
  Bidding = 'bidding',
  InProgress = 'in_progress',
  PendingReview = 'pending_review',
  Completed = 'completed',
  Cancelled = 'cancelled'
}

// 报价状态（与后端 backend/internal/constants/bid_status.go 对齐）
export enum BidStatus {
  Pending = 'pending',
  Accepted = 'accepted',
  Rejected = 'rejected',
  Withdrawn = 'withdrawn'
}

// 合同状态（与后端 backend/internal/constants/contract_status.go 对齐）
export enum ContractStatus {
  PendingSignature = 'pending_signature',
  InProgress = 'in_progress',
  PendingReview = 'pending_review',
  Completed = 'completed',
  Terminated = 'terminated'
}

// 用户角色（与后端 backend/internal/constants/roles.go 对齐）
export enum UserRole {
  Requester = 'requester',
  Freelancer = 'freelancer',
  Both = 'both',
  Admin = 'admin'
}

// 合同争议状态（与后端 backend/internal/constants/dispute_status.go 对齐）
export enum DisputeStatus {
  Submitted = 'submitted',
  Accepted = 'accepted',
  AwaitingSupplement = 'awaiting_supplement',
  Ruled = 'ruled'
}

// 裁决责任方（与后端 dto.RuleDisputeRequest.rulingParty 对齐）
export enum RulingParty {
  PartyA = 'party_a',
  PartyB = 'party_b',
  Shared = 'shared'
}

export const RequirementStatusLabel: Record<string, string> = {
  draft: '草稿',
  open: '待报价',
  bidding: '报价中',
  in_progress: '进行中',
  pending_review: '待验收',
  completed: '已完成',
  cancelled: '已取消'
};

export const BidStatusLabel: Record<string, string> = {
  pending: '待审',
  accepted: '已采纳',
  rejected: '已拒绝',
  withdrawn: '已撤回'
};

export const ContractStatusLabel: Record<string, string> = {
  pending_signature: '待签署',
  in_progress: '执行中',
  pending_review: '待验收',
  completed: '已完成',
  terminated: '已终止'
};

export const RoleLabel: Record<string, string> = {
  requester: '需求方',
  freelancer: '自由职业者',
  both: '双角色',
  admin: '管理员'
};

export const DisputeStatusLabel: Record<string, string> = {
  submitted: '待受理',
  accepted: '处理中',
  awaiting_supplement: '待补充材料',
  ruled: '已裁决'
};

export const RulingPartyLabel: Record<string, string> = {
  party_a: '甲方责任',
  party_b: '乙方责任',
  shared: '双方分担'
};

// 后端争议专用错误码（与 backend/internal/constants/dispute_errors.go 对齐）
export const DisputeErrorCode = {
  NotParty: 40310,
  NotAdmin: 40311,
  AlreadyOpen: 40910,
  IllegalTransition: 40911,
  Closed: 40912,
  Concurrent: 40913
} as const;
