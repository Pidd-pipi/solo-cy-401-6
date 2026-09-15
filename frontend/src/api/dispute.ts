import { getData, postData } from './request';
import type { Dispute } from '../types';

export interface CreateDisputePayload {
  reason: string;
  claim: string;
  evidence: string[];
}

export interface RuleDisputePayload {
  responsibility: string;
  rulingParty: string;
  refundAmount: number;
  opinion: string;
}

export const disputeApi = {
  // 当事方对某合同提交争议
  file: (contractId: number, payload: CreateDisputePayload) =>
    postData<Dispute>(`/contracts/${contractId}/disputes`, payload),
  // 某合同的争议历史（当事方/管理员）
  listByContract: (contractId: number) =>
    getData<Dispute[]>(`/contracts/${contractId}/disputes`),
  // 争议列表：管理员看全部，当事方看自己相关
  list: () => getData<Dispute[]>('/disputes'),
  detail: (id: number) => getData<Dispute>(`/disputes/${id}`),
  accept: (id: number) => postData<Dispute>(`/disputes/${id}/accept`),
  requestSupplement: (id: number, note: string) =>
    postData<Dispute>(`/disputes/${id}/request-supplement`, { note }),
  supplement: (id: number, content: string, attachments: string[]) =>
    postData<Dispute>(`/disputes/${id}/supplement`, { content, attachments }),
  rule: (id: number, payload: RuleDisputePayload) =>
    postData<Dispute>(`/disputes/${id}/rule`, payload)
};
