import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import ContractCard from './ContractCard.vue';
import type { Contract } from '../../types';
import { DisputeStatus } from '../../types/enums';
import { formatCurrency } from '../../utils/formatCurrency';

function baseContract(over: Partial<Contract> = {}): Contract {
  return {
    id: 1,
    contractNo: 'CY-1-1',
    totalAmount: 45000,
    paymentType: 'installments',
    stages: [],
    status: 'in_progress',
    requirementId: 1,
    partyAId: 10,
    partyBId: 20,
    ...over
  };
}

// ContractCard is consumed by the workbench. It must show the in-process
// dispute while one exists, and keep showing the latest ruling after closing.
describe('ContractCard dispute echo', () => {
  it('shows the in-process dispute strip (and no verdict) while a dispute is open', () => {
    const c = baseContract({
      activeDispute: {
        id: 7, contractId: 1, complainantId: 10, reason: 'r', claim: '要求退还部分款项',
        evidence: [], status: DisputeStatus.Submitted
      } as Contract['activeDispute'],
      lastRuling: null
    });
    const w = mount(ContractCard, { props: { contract: c }, global: { config: { globalProperties: { $router: { push: () => {} } } } as any } });
    const text = w.text();
    expect(text).toContain('要求退还部分款项');
    expect(text).toContain('待受理');
    expect(text).not.toContain('最近裁决');
  });

  it('keeps showing the latest ruling (party + refund + 已裁决) after the dispute closes', () => {
    const c = baseContract({
      activeDispute: null,
      lastRuling: {
        id: 7, contractId: 1, complainantId: 10, reason: 'r', claim: 'c', evidence: [],
        status: DisputeStatus.Ruled, rulingParty: 'party_b', refundAmount: 15000,
        responsibility: '乙方主责', opinion: '意见', ruledAt: '2026-09-15T08:00:00Z'
      } as Contract['lastRuling']
    });
    const w = mount(ContractCard, { props: { contract: c } });
    const text = w.text();
    expect(text).toContain('已裁决');
    expect(text).toContain('最近裁决');
    expect(text).toContain('乙方责任');
    expect(text).toContain(formatCurrency(15000));
    expect(text).not.toContain('待受理');
  });

  it('shows nothing dispute-related when there is neither an open dispute nor a ruling', () => {
    const w = mount(ContractCard, { props: { contract: baseContract({ activeDispute: null, lastRuling: null }) } });
    const text = w.text();
    expect(text).not.toContain('最近裁决');
    expect(text).not.toContain('待受理');
    expect(text).not.toContain('已裁决');
  });
});
