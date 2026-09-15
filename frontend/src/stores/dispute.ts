import { defineStore } from 'pinia';
import type { Dispute } from '../types';
import { disputeApi } from '../api/dispute';

export const useDisputeStore = defineStore('dispute', {
  state: () => ({
    disputes: [] as Dispute[],
    loading: false
  }),
  getters: {
    openCount: (state) => state.disputes.filter((d) => d.status !== 'ruled').length
  },
  actions: {
    async fetchList() {
      this.loading = true;
      try {
        this.disputes = await disputeApi.list();
        return this.disputes;
      } finally {
        this.loading = false;
      }
    },
    async fetchByContract(contractId: number) {
      return disputeApi.listByContract(contractId);
    }
  }
});
