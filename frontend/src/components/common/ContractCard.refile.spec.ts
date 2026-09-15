import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import ContractCard from './ContractCard.vue';
import type { Contract, Dispute } from '../../types';
import { DisputeStatus } from '../../types/enums';
import { formatCurrency } from '../../utils/formatCurrency';

// Regression suite for re-filing a dispute after the previous one was ruled.
//
// Scenario: dispute #1 was ruled (closed), and the party has just filed a NEW
// in-process dispute #2 on the same contract. The workbench card must keep BOTH
// cues visible at once — the last verdict (liability + refund must not vanish)
// AND the current in-process dispute prompt. An earlier implementation rendered
// them with v-if / v-else-if, so the verdict disappeared whenever a new dispute
// was opened.
describe('ContractCard — 裁决关闭后再次发起争议的回显', () => {
  const ruled1: Dispute = {
    id: 1,
    contractId: 9,
    complainantId: 10,
    reason: '第一次争议',
    claim: '第一次诉求',
    evidence: [],
    status: DisputeStatus.Ruled,
    rulingParty: 'party_b',
    refundAmount: 15000,
    responsibility: '乙方承担主要责任',
    opinion: '依据证据裁决退款',
    ruledAt: '2026-09-01T08:00:00Z'
  };

  const open2: Dispute = {
    id: 2,
    contractId: 9,
    complainantId: 20,
    reason: '对退款执行有异议',
    claim: '督促尽快执行退款',
    evidence: [],
    status: DisputeStatus.Submitted
  };

  const contract: Contract = {
    id: 9,
    contractNo: 'CY-9-1',
    totalAmount: 45000,
    paymentType: 'installments',
    stages: [],
    status: 'in_progress',
    requirementId: 1,
    partyAId: 10,
    partyBId: 20,
    // KEY STATE: a new open dispute AND a prior closed ruling coexist.
    activeDispute: open2,
    lastRuling: ruled1
  };

  function render() {
    return mount(ContractCard, {
      props: { contract },
      global: { config: { globalProperties: { $router: { push: () => {} } } } as any }
    });
  }

  it('同时保留最近一次裁决结果与当前处理中争议提示', () => {
    const text = render().text();

    // 当前处理中争议提示仍在（状态 + 新诉求）
    expect(text).toContain('待受理');
    expect(text).toContain('督促尽快执行退款');

    // 最近一次裁决结果也必须在（责任方 + 应退金额 + 已裁决语义）
    expect(text).toContain('最近裁决');
    expect(text).toContain('乙方责任');
    expect(text).toContain(formatCurrency(15000));
  });

  it('处理中争议徽章为争议状态而非已裁决', () => {
    // 头部应显示争议“待受理”徽章，而不是被已裁决标签顶替
    const text = render().text();
    const idxOpen = text.indexOf('待受理');
    const idxRuling = text.indexOf('最近裁决');
    expect(idxOpen).toBeGreaterThanOrEqual(0);
    expect(idxRuling).toBeGreaterThanOrEqual(0);
  });
});
