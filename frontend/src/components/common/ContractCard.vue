<template>
  <el-card class="contract-card" shadow="hover" @click="$router.push(`/contracts/${contract.id}`)">
    <div class="c-head">
      <b>{{ contract.contractNo }}</b>
      <div class="badges">
        <StatusBadge v-if="contract.activeDispute" :status="contract.activeDispute.status" kind="dispute" />
        <el-tag v-else-if="contract.lastRuling" type="success" size="small" effect="light">已裁决</el-tag>
        <StatusBadge :status="contract.status" kind="contract" />
      </div>
    </div>
    <p class="muted">{{ contract.requirement?.title || '相关需求' }}</p>
    <p><b>{{ formatCurrency(contract.totalAmount) }}</b>
      <span class="muted"> · {{ contract.paymentType === 'installments' ? '分阶段付款' : '一次性付款' }}</span>
    </p>

    <!-- 处理中争议提示 -->
    <div v-if="contract.activeDispute" class="dispute-strip open">
      <el-icon><Warning /></el-icon>
      <span class="strip-text">
        {{ DisputeStatusLabel[contract.activeDispute.status] || '争议处理中' }}：{{ contract.activeDispute.claim }}
      </span>
    </div>

    <!-- 最近一次裁决结果：独立渲染（非 v-else-if），与新发起的处理中争议并存，关闭后不消失 -->
    <div v-if="contract.lastRuling" class="dispute-strip ruled">
      <el-icon><CircleCheck /></el-icon>
      <span class="strip-text">
        最近裁决 · {{ RulingPartyLabel[contract.lastRuling.rulingParty || ''] || '责任已划分' }}
        · 应退 {{ formatCurrency(contract.lastRuling.refundAmount || 0) }}
      </span>
    </div>

    <div class="c-parties muted">
      <span>{{ contract.partyA?.name }}（甲方）</span>
      <span>↔</span>
      <span>{{ contract.partyB?.name }}（乙方）</span>
    </div>
  </el-card>
</template>

<script setup lang="ts">
import type { Contract } from '../../types';
import StatusBadge from './StatusBadge.vue';
import { formatCurrency } from '../../utils/formatCurrency';
import { DisputeStatusLabel, RulingPartyLabel } from '../../types/enums';
import { Warning, CircleCheck } from '@element-plus/icons-vue';

defineProps<{ contract: Contract }>();
</script>

<style scoped>
.contract-card { margin-bottom: 16px; cursor: pointer; }
.c-head { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; gap: 8px; }
.badges { display: flex; gap: 6px; }
.c-parties { display: flex; gap: 8px; margin-top: 8px; }
.dispute-strip {
  display: flex; align-items: center; gap: 6px;
  margin-top: 8px; padding: 6px 10px; border-radius: 6px; font-size: 13px; line-height: 1.5;
}
.dispute-strip .strip-text { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.dispute-strip.open { background: #fdf6ec; color: #b88230; }
.dispute-strip.ruled { background: #f0f9eb; color: #5daf34; }
</style>
