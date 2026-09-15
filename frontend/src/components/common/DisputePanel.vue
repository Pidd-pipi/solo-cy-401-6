<template>
  <el-card class="dispute-card">
    <template #header>
      <div class="d-head">
        <b>合同争议</b>
        <el-tag v-if="active" :type="activeKind" size="small" effect="dark">
          {{ DisputeStatusLabel[active.status] || active.status }}
        </el-tag>
        <span v-else class="muted">暂无处理中的争议</span>
      </div>
    </template>

    <!-- 当前处理中的争议 -->
    <div v-if="active" class="active">
      <div class="row"><span class="muted">发起人：</span>{{ active.complainant?.name || ('用户#' + active.complainantId) }}</div>
      <div class="row"><span class="muted">争议事由：</span>{{ active.reason }}</div>
      <div class="row"><span class="muted">诉求：</span>{{ active.claim }}</div>
      <div v-if="active.evidence?.length" class="row">
        <span class="muted">证据：</span>
        <span v-for="(ev, i) in active.evidence" :key="i" class="chip">{{ ev }}</span>
      </div>
      <div v-if="active.supplementNote" class="row note">
        <span class="muted">补充要求：</span>{{ active.supplementNote }}
      </div>

      <!-- 补充材料记录 -->
      <el-collapse v-if="active.supplements?.length" class="rounds">
        <el-collapse-item :title="`补充材料（${active.supplements.length} 轮）`" name="rounds">
          <div v-for="r in active.supplements" :key="r.id" class="round">
            <div class="muted">{{ r.submitter?.name || ('用户#' + r.submitterId) }} · {{ formatTime(r.createdAt) }}</div>
            <div>{{ r.content }}</div>
            <div v-if="r.attachments?.length">
              <span v-for="(a, i) in r.attachments" :key="i" class="chip">{{ a }}</span>
            </div>
          </div>
        </el-collapse-item>
      </el-collapse>

      <!-- 当事方动作 -->
      <div v-if="isParty" class="actions">
        <el-button
          v-if="active.status === DisputeStatus.AwaitingSupplement"
          type="warning"
          @click="openSupplement"
        >补充材料</el-button>
      </div>

      <!-- 管理员动作 -->
      <div v-if="isAdmin" class="actions">
        <el-button
          v-if="active.status === DisputeStatus.Submitted"
          type="primary"
          :loading="busy"
          @click="onAccept"
        >受理</el-button>
        <el-button
          v-if="adminOpenStates.includes(active.status)"
          @click="askSupplement = true"
        >要求补充</el-button>
        <el-button
          v-if="active.status === DisputeStatus.Accepted"
          type="danger"
          @click="openRule"
        >裁决</el-button>
      </div>
    </div>

    <!-- 无进行中争议：当事方提交入口 -->
    <div v-else-if="isParty" class="actions">
      <el-button type="primary" @click="openFile">提交争议</el-button>
      <span class="muted tip">同一合同仅允许一条处理中的争议，裁决关闭后不可再改。</span>
    </div>

    <!-- 已裁决历史 -->
    <template v-if="closed.length">
      <el-divider />
      <div class="history-title muted">历史裁决（{{ closed.length }}）</div>
      <el-timeline class="history">
        <el-timeline-item v-for="d in closed" :key="d.id" :timestamp="formatTime(d.ruledAt)" placement="top" type="success">
          <div class="verdict">
            <div><b>责任划分：</b>{{ d.responsibility }}
              <el-tag size="small" class="vtag">{{ RulingPartyLabel[d.rulingParty || ''] || d.rulingParty }}</el-tag>
            </div>
            <div><b>应退金额：</b>{{ formatCurrency(d.refundAmount || 0) }}</div>
            <div><b>处理意见：</b>{{ d.opinion }}</div>
            <div class="muted">裁决人：{{ d.admin?.name || ('管理员#' + d.adminId) }} · 裁决时间 {{ formatTime(d.ruledAt) }}</div>
          </div>
        </el-timeline-item>
      </el-timeline>
    </template>

    <!-- 提交争议弹窗 -->
    <el-dialog v-model="fileDialog" title="提交合同争议" width="520px">
      <el-form :model="fileForm" label-width="88px">
        <el-form-item label="争议事由" required>
          <el-input v-model="fileForm.reason" type="textarea" :rows="3" maxlength="500" show-word-limit />
        </el-form-item>
        <el-form-item label="诉求" required>
          <el-input v-model="fileForm.claim" type="textarea" :rows="2" maxlength="500" show-word-limit />
        </el-form-item>
        <el-form-item label="证据">
          <el-input v-model="fileForm.evidenceText" type="textarea" :rows="3" placeholder="每行一条证据名称或链接" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="fileDialog = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="onFile">提交</el-button>
      </template>
    </el-dialog>

    <!-- 补充材料弹窗 -->
    <el-dialog v-model="supplementDialog" title="补充争议材料" width="520px">
      <el-form label-width="88px">
        <el-form-item label="补充说明" required>
          <el-input v-model="supplementContent" type="textarea" :rows="3" maxlength="500" show-word-limit />
        </el-form-item>
        <el-form-item label="附件">
          <el-input v-model="supplementAttachText" type="textarea" :rows="2" placeholder="每行一条附件名称或链接" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="supplementDialog = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="onSupplement">提交补充</el-button>
      </template>
    </el-dialog>

    <!-- 要求补充弹窗（管理员） -->
    <el-dialog v-model="askSupplement" title="要求补充材料" width="480px">
      <el-input v-model="supplementNote" type="textarea" :rows="3" maxlength="500" show-word-limit placeholder="说明需要当事方补充的材料" />
      <template #footer>
        <el-button @click="askSupplement = false">取消</el-button>
        <el-button type="primary" :loading="busy" @click="onRequestSupplement">发送要求</el-button>
      </template>
    </el-dialog>

    <!-- 裁决弹窗（管理员） -->
    <el-dialog v-model="ruleDialog" title="争议裁决" width="560px">
      <el-form :model="ruleForm" :rules="ruleRules" ref="ruleFormRef" label-width="96px">
        <el-form-item label="责任划分" prop="responsibility">
          <el-input v-model="ruleForm.responsibility" type="textarea" :rows="2" maxlength="500" show-word-limit />
        </el-form-item>
        <el-form-item label="责任方" prop="rulingParty">
          <el-radio-group v-model="ruleForm.rulingParty">
            <el-radio value="party_a">甲方责任</el-radio>
            <el-radio value="party_b">乙方责任</el-radio>
            <el-radio value="shared">双方分担</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="应退金额" prop="refundAmount">
          <el-input-number v-model="ruleForm.refundAmount" :min="0" :precision="2" :step="100" />
        </el-form-item>
        <el-form-item label="处理意见" prop="opinion">
          <el-input v-model="ruleForm.opinion" type="textarea" :rows="3" maxlength="1000" show-word-limit />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="ruleDialog = false">取消</el-button>
        <el-button type="danger" :loading="busy" @click="onRule">作出裁决（关闭争议）</el-button>
      </template>
    </el-dialog>
  </el-card>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue';
import type { FormInstance, FormRules } from 'element-plus';
import { ElMessage } from 'element-plus';
import type { Contract, Dispute } from '../../types';
import {
  DisputeStatus,
  DisputeStatusLabel,
  RulingPartyLabel
} from '../../types/enums';
import { disputeApi } from '../../api/dispute';
import { useUserStore } from '../../stores/user';
import { formatCurrency } from '../../utils/formatCurrency';

const props = defineProps<{ contract: Contract }>();
const emit = defineEmits<{ (e: 'changed'): void }>();

const userStore = useUserStore();
const disputes = ref<Dispute[]>([]);
const busy = ref(false);

const isAdmin = computed(() => userStore.isAdmin);
// 管理员可“要求补充”的状态（待受理/处理中）
const adminOpenStates: string[] = [DisputeStatus.Submitted, DisputeStatus.Accepted];
const isParty = computed(
  () =>
    !!userStore.user &&
    (userStore.user.id === props.contract.partyAId || userStore.user.id === props.contract.partyBId)
);

const active = computed<Dispute | null>(() => {
  const open = disputes.value.find((d) => d.status !== 'ruled');
  return open || props.contract.activeDispute || null;
});
const closed = computed(() => disputes.value.filter((d) => d.status === 'ruled'));

const activeKind = computed<'warning' | 'primary' | 'success'>(() => {
  switch (active.value?.status) {
    case DisputeStatus.Accepted:
      return 'primary';
    case DisputeStatus.Ruled:
      return 'success';
    default:
      return 'warning';
  }
});

async function load() {
  try {
    disputes.value = await disputeApi.listByContract(props.contract.id);
  } catch {
    // 无权限（非当事方/非管理员）时静默，面板不展示操作
    disputes.value = [];
  }
}

function splitLines(text: string): string[] {
  return text
    .split(/\r?\n|,|，/)
    .map((s) => s.trim())
    .filter(Boolean);
}

function formatTime(v?: string | null): string {
  if (!v) return '-';
  return new Date(v).toLocaleString();
}

// ---- 提交争议 ----
const fileDialog = ref(false);
const fileForm = reactive({ reason: '', claim: '', evidenceText: '' });
function openFile() {
  fileForm.reason = '';
  fileForm.claim = '';
  fileForm.evidenceText = '';
  fileDialog.value = true;
}
async function onFile() {
  if (fileForm.reason.trim().length < 5 || fileForm.claim.trim().length < 2) {
    ElMessage.warning('请填写完整的争议事由（≥5字）和诉求');
    return;
  }
  busy.value = true;
  try {
    await disputeApi.file(props.contract.id, {
      reason: fileForm.reason.trim(),
      claim: fileForm.claim.trim(),
      evidence: splitLines(fileForm.evidenceText)
    });
    ElMessage.success('争议已提交');
    fileDialog.value = false;
    await load();
    emit('changed');
  } finally {
    busy.value = false;
  }
}

// ---- 补充材料 ----
const supplementDialog = ref(false);
const supplementContent = ref('');
const supplementAttachText = ref('');
function openSupplement() {
  supplementContent.value = '';
  supplementAttachText.value = '';
  supplementDialog.value = true;
}
async function onSupplement() {
  if (!active.value || supplementContent.value.trim().length < 2) {
    ElMessage.warning('请填写补充说明');
    return;
  }
  busy.value = true;
  try {
    await disputeApi.supplement(
      active.value.id,
      supplementContent.value.trim(),
      splitLines(supplementAttachText.value)
    );
    ElMessage.success('补充材料已提交，等待管理员处理');
    supplementDialog.value = false;
    await load();
    emit('changed');
  } finally {
    busy.value = false;
  }
}

// ---- 管理员：要求补充 ----
const askSupplement = ref(false);
const supplementNote = ref('');
async function onRequestSupplement() {
  if (!active.value || supplementNote.value.trim().length < 2) {
    ElMessage.warning('请填写补充要求');
    return;
  }
  busy.value = true;
  try {
    await disputeApi.requestSupplement(active.value.id, supplementNote.value.trim());
    ElMessage.success('已要求当事方补充材料');
    askSupplement.value = false;
    await load();
    emit('changed');
  } finally {
    busy.value = false;
  }
}

// ---- 管理员：受理 ----
async function onAccept() {
  if (!active.value) return;
  busy.value = true;
  try {
    await disputeApi.accept(active.value.id);
    ElMessage.success('已受理');
    await load();
    emit('changed');
  } finally {
    busy.value = false;
  }
}

// ---- 管理员：裁决 ----
const ruleDialog = ref(false);
const ruleFormRef = ref<FormInstance>();
const ruleForm = reactive({
  responsibility: '',
  rulingParty: 'shared',
  refundAmount: 0,
  opinion: ''
});
const ruleRules: FormRules = {
  responsibility: [{ required: true, min: 2, message: '请填写责任划分', trigger: 'blur' }],
  rulingParty: [{ required: true, message: '请选择责任方', trigger: 'change' }],
  opinion: [{ required: true, min: 2, message: '请填写处理意见', trigger: 'blur' }]
};
function openRule() {
  ruleForm.responsibility = '';
  ruleForm.rulingParty = 'shared';
  ruleForm.refundAmount = 0;
  ruleForm.opinion = '';
  ruleDialog.value = true;
}
async function onRule() {
  if (!active.value || !ruleFormRef.value) return;
  const valid = await ruleFormRef.value.validate().catch(() => false);
  if (!valid) return;
  busy.value = true;
  try {
    await disputeApi.rule(active.value.id, { ...ruleForm });
    ElMessage.success('已作出裁决，争议关闭');
    ruleDialog.value = false;
    await load();
    emit('changed');
  } finally {
    busy.value = false;
  }
}

onMounted(() => void load());

defineExpose({ reload: load });
</script>

<style scoped>
.dispute-card { margin-top: 16px; }
.d-head { display: flex; align-items: center; gap: 12px; }
.row { margin: 6px 0; line-height: 1.6; }
.chip { display: inline-block; background: #f0f2f5; border-radius: 4px; padding: 1px 8px; margin: 2px 6px 2px 0; font-size: 12px; }
.note { background: #fdf6ec; border-radius: 4px; padding: 6px 8px; }
.rounds { margin-top: 8px; }
.round { margin-bottom: 10px; }
.actions { margin-top: 14px; display: flex; align-items: center; gap: 12px; }
.tip { font-size: 12px; }
.history-title { margin-bottom: 10px; }
.verdict { line-height: 1.8; }
.vtag { margin-left: 8px; }
</style>
