const base = process.env.APP_BASE_URL || "http://localhost:18080";
const email = process.env.ADMIN_EMAIL;
const password = process.env.ADMIN_PASSWORD;
const memberPassword = "member-password";

if (!email || !password) {
  console.error("ADMIN_EMAIL and ADMIN_PASSWORD must be set before seeding.");
  process.exit(1);
}

const demoMembers = [
  { no: "M-DEMO-001", name: "Siti Rahmawati", email: "siti.demo@coop.test", phone: "081234567890", address: "Jakarta", join: "2026-01-15" },
  { no: "M-DEMO-002", name: "Budi Santoso", email: "budi.demo@coop.test", phone: "081234567891", address: "Bogor", join: "2026-01-20" },
  { no: "M-DEMO-003", name: "Dewi Lestari", email: "dewi.demo@coop.test", phone: "081234567892", address: "Depok", join: "2026-01-22" },
  { no: "M-DEMO-004", name: "Agus Wijaya", email: "agus.demo@coop.test", phone: "081234567893", address: "Tangerang", join: "2026-02-02" },
  { no: "M-DEMO-005", name: "Rina Marlina", email: "rina.demo@coop.test", phone: "081234567894", address: "Bekasi", join: "2026-02-06" },
  { no: "M-DEMO-006", name: "Hendra Gunawan", email: "hendra.demo@coop.test", phone: "081234567895", address: "Jakarta", join: "2026-02-12" },
  { no: "M-DEMO-007", name: "Maya Prasetya", email: "maya.demo@coop.test", phone: "081234567896", address: "Bogor", join: "2026-02-18" },
  { no: "M-DEMO-008", name: "Fajar Nugroho", email: "fajar.demo@coop.test", phone: "081234567897", address: "Depok", join: "2026-03-01" },
];

const savingPlans = {
  "M-DEMO-001": [
    ["deposit", "pokok", 100000, "2026-01-15", "DEMO-001-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 500000, "2026-02-01", "DEMO-001-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 1500000, "2026-02-15", "DEMO-001-SUK-01", "Simpanan sukarela demo"],
    ["deposit", "wajib", 500000, "2026-03-01", "DEMO-001-WJB-02", "Simpanan wajib Maret"],
  ],
  "M-DEMO-002": [
    ["deposit", "pokok", 100000, "2026-01-20", "DEMO-002-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 350000, "2026-02-03", "DEMO-002-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 900000, "2026-02-18", "DEMO-002-SUK-01", "Simpanan sukarela demo"],
    ["deposit", "wajib", 350000, "2026-03-03", "DEMO-002-WJB-02", "Simpanan wajib Maret"],
  ],
  "M-DEMO-003": [
    ["deposit", "pokok", 100000, "2026-01-22", "DEMO-003-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 300000, "2026-02-05", "DEMO-003-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 750000, "2026-02-20", "DEMO-003-SUK-01", "Simpanan sukarela demo"],
  ],
  "M-DEMO-004": [
    ["deposit", "pokok", 100000, "2026-02-02", "DEMO-004-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 250000, "2026-02-10", "DEMO-004-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 650000, "2026-03-10", "DEMO-004-SUK-01", "Simpanan sukarela demo"],
  ],
  "M-DEMO-005": [
    ["deposit", "pokok", 100000, "2026-02-06", "DEMO-005-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 275000, "2026-02-12", "DEMO-005-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 825000, "2026-03-12", "DEMO-005-SUK-01", "Simpanan sukarela demo"],
  ],
  "M-DEMO-006": [
    ["deposit", "pokok", 100000, "2026-02-12", "DEMO-006-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 225000, "2026-02-18", "DEMO-006-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 500000, "2026-03-18", "DEMO-006-SUK-01", "Simpanan sukarela demo"],
  ],
  "M-DEMO-007": [
    ["deposit", "pokok", 100000, "2026-02-18", "DEMO-007-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 325000, "2026-02-24", "DEMO-007-WJB-01", "Simpanan wajib Februari"],
    ["deposit", "sukarela", 1100000, "2026-03-24", "DEMO-007-SUK-01", "Simpanan sukarela demo"],
  ],
  "M-DEMO-008": [
    ["deposit", "pokok", 100000, "2026-03-01", "DEMO-008-POK", "Simpanan pokok awal"],
    ["deposit", "wajib", 200000, "2026-03-08", "DEMO-008-WJB-01", "Simpanan wajib Maret"],
    ["deposit", "sukarela", 450000, "2026-04-08", "DEMO-008-SUK-01", "Simpanan sukarela demo"],
  ],
};

const withdrawalPlans = [
  { member: "M-DEMO-001", amount: 200000, note: "Demo pending withdrawal" },
  { member: "M-DEMO-004", amount: 150000, note: "Demo biaya sekolah" },
  { member: "M-DEMO-007", amount: 250000, note: "Demo kebutuhan keluarga" },
];

const loanPlans = [
  {
    member: "M-DEMO-001",
    requested: 1200000,
    approved: 1200000,
    duration: 6,
    purpose: "Demo working capital loan",
    repayments: [
      [200000, "2026-03-01", "DEMO-001-RPY-001", "Demo repayment 1"],
      [200000, "2026-04-01", "DEMO-001-RPY-002", "Demo repayment 2"],
    ],
  },
  {
    member: "M-DEMO-002",
    requested: 1800000,
    approved: 1500000,
    duration: 10,
    purpose: "Demo home renovation loan",
    repayments: [
      [150000, "2026-04-05", "DEMO-002-RPY-001", "Renovation installment 1"],
      [150000, "2026-05-05", "DEMO-002-RPY-002", "Renovation installment 2"],
    ],
  },
  {
    member: "M-DEMO-005",
    requested: 900000,
    approved: 900000,
    duration: 9,
    purpose: "Demo education loan",
    repayments: [[100000, "2026-05-10", "DEMO-005-RPY-001", "Education installment 1"]],
  },
];

const pendingLoanPlans = [
  { member: "M-DEMO-006", requested: 750000, duration: 5, purpose: "Demo pending medical loan" },
  { member: "M-DEMO-008", requested: 600000, duration: 6, purpose: "Demo pending appliance loan" },
];

async function request(path, options = {}) {
  const res = await fetch(base + path, {
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...(options.headers || {}),
    },
  });
  const text = await res.text();
  let body;
  try {
    body = text ? JSON.parse(text) : {};
  } catch {
    body = text;
  }
  if (!res.ok) {
    const msg = typeof body === "string" ? body : JSON.stringify(body);
    throw new Error(`${options.method || "GET"} ${path} failed ${res.status}: ${msg}`);
  }
  return body;
}

async function loginAs(loginEmail, loginPassword) {
  const login = await request("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: loginEmail, password: loginPassword }),
  });
  return { Authorization: `Bearer ${login.token}` };
}

async function main() {
  const auth = await loginAs(email, password);

  const members = await request("/api/admin/members", { headers: auth });
  const memberByNo = new Map((members.members || []).map((m) => [m.member_no, m]));

  async function ensureMember(def) {
    if (memberByNo.has(def.no)) return memberByNo.get(def.no);
    const created = await request("/api/admin/members", {
      method: "POST",
      headers: auth,
      body: JSON.stringify({
        member_no: def.no,
        full_name: def.name,
        phone: def.phone,
        address: def.address,
        join_date: def.join,
        status: "active",
        email: def.email,
        password: memberPassword,
      }),
    });
    memberByNo.set(def.no, created);
    return created;
  }

  for (const member of demoMembers) {
    await ensureMember(member);
  }

  const savings = await request("/api/admin/savings", { headers: auth });
  const savingRefs = new Set((savings.savings || []).map((s) => s.reference_no));

  async function ensureSaving(member, type, category, amount, date, referenceNo, note) {
    if (savingRefs.has(referenceNo)) return;
    await request("/api/admin/savings", {
      method: "POST",
      headers: auth,
      body: JSON.stringify({
        member_id: member.id,
        type,
        category,
        amount,
        record_date: date,
        reference_no: referenceNo,
        note,
      }),
    });
    savingRefs.add(referenceNo);
  }

  for (const [memberNo, rows] of Object.entries(savingPlans)) {
    const member = memberByNo.get(memberNo);
    for (const row of rows) {
      await ensureSaving(member, ...row);
    }
  }

  const authByMemberNo = new Map();
  async function memberAuth(memberNo) {
    if (authByMemberNo.has(memberNo)) return authByMemberNo.get(memberNo);
    const def = demoMembers.find((m) => m.no === memberNo);
    const token = await loginAs(def.email, memberPassword);
    authByMemberNo.set(memberNo, token);
    return token;
  }

  for (const plan of withdrawalPlans) {
    const headers = await memberAuth(plan.member);
    const withdrawals = await request("/api/member/withdrawal-requests", { headers });
    if (!(withdrawals.withdrawal_requests || []).some((w) => w.note === plan.note)) {
      await request("/api/member/withdrawal-requests", {
        method: "POST",
        headers,
        body: JSON.stringify({ amount: plan.amount, note: plan.note }),
      });
    }
  }

  async function ensureLoanRequest(plan) {
    const headers = await memberAuth(plan.member);
    const requests = await request("/api/member/loan-requests", { headers });
    let loanRequest = (requests.loan_requests || []).find((r) => r.purpose === plan.purpose);
    if (!loanRequest) {
      loanRequest = await request("/api/member/loan-requests", {
        method: "POST",
        headers,
        body: JSON.stringify({
          requested_amount: plan.requested,
          duration_months: plan.duration,
          purpose: plan.purpose,
        }),
      });
    }
    return loanRequest;
  }

  for (const plan of loanPlans) {
    const member = memberByNo.get(plan.member);
    const loanRequest = await ensureLoanRequest(plan);
    const loansBefore = await request("/api/admin/loans", { headers: auth });
    let activeLoan = (loansBefore.loans || []).find((l) => l.member_id === member.id && l.approved_amount === plan.approved);
    if (!activeLoan && loanRequest.status === "pending") {
      activeLoan = await request(`/api/admin/loan-requests/${loanRequest.id}/approve`, {
        method: "POST",
        headers: auth,
        body: JSON.stringify({ approved_amount: plan.approved, duration_months: plan.duration }),
      });
    }
    if (!activeLoan) continue;

    const headers = await memberAuth(plan.member);
    const repayments = await request("/api/member/repayments", { headers });
    const repaymentRefs = new Set((repayments.repayments || []).map((r) => r.reference_no));
    for (const [amount, date, referenceNo, note] of plan.repayments) {
      if (repaymentRefs.has(referenceNo)) continue;
      await request(`/api/admin/loans/${activeLoan.id}/repayments`, {
        method: "POST",
        headers: auth,
        body: JSON.stringify({ amount, record_date: date, reference_no: referenceNo, note }),
      });
      repaymentRefs.add(referenceNo);
    }
  }

  for (const plan of pendingLoanPlans) {
    await ensureLoanRequest(plan);
  }

  const dashboard = await request("/api/admin/dashboard", { headers: auth });
  console.log(JSON.stringify({
    seeded_members: demoMembers.map((m) => m.no),
    total_members: dashboard.total_members,
    total_savings: dashboard.total_savings,
    active_loans: dashboard.active_loans,
    outstanding_loan: dashboard.total_outstanding_loan,
    pending_requests: dashboard.pending_loan_requests,
  }, null, 2));
}

main().catch((err) => {
  console.error(err.message);
  process.exit(1);
});
