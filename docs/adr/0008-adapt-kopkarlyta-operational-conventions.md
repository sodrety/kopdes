# Adapt KOPKARLYTA operational conventions without full parity

Accepted. Kopdes will move toward KOPKARLYTA-inspired cooperative back-office conventions for Bahasa-first labels, grouped admin/sidebar layout categories, Simpanan, Penarikan, Pinjaman, Angsuran workflows, filters, exports, reports, and member/user operational feel, while keeping the current Go server-rendered htmx architecture. The visual tone should become brighter and closer to a cooperative operations dashboard, without copying KOPKARLYTA's exact Bootstrap/AdminLTE theme. Use a light palette with white or soft-gray surfaces, bright green as the primary cooperative color, blue or teal accents for Pinjaman, amber for pending states, and red for rejected or danger states. Prefer a lighter sidebar treatment over a dark admin shell. This is not full KOPKARLYTA parity: payment-provider settings, Transaksi Kas, Piutang, SHU, Neraca, Laba Rugi, public CMS/gallery, and public self-registration remain out of scope until separate product decisions accept them.

The first implementation slice should prioritize Admin Simpanan, Penarikan, Pinjaman, and Angsuran back-office workflows before member-facing polish, because the KOPKARLYTA-like operational feel is mostly expressed through admin-side data entry, filters, status handling, exports, and reports. Penarikan means Admin-reviewed withdrawal requests from Simpanan Sukarela only; the app records verified external activity and does not move money. Browser workflow statuses should use Bahasa labels such as Menunggu, Disetujui, Ditolak, and Selesai while stable internal enum/code names remain in English. Reporting should start with simple operational CSV exports for filtered Simpanan, Penarikan, Pinjaman, and Angsuran rows rather than formal accounting statements.

**Consequences**

- Use Bahasa-first product labels such as Simpanan, Pinjaman, and Angsuran in browser UI while preserving precise domain terms in code and documentation.
- Use grouped sidebar sections for in-scope modules only; do not show disabled links for unsupported accounting or public-site modules.
- Prioritize Admin operational workflows before broader member-facing UI polish.
- Model Penarikan as Admin-reviewed withdrawal requests from Simpanan Sukarela only.
- Display Bahasa status labels while keeping stable English internal status names.
- Implement simple operational CSV exports before formal financial statements.
- Retain the record-verified-external-activity boundary unless a later ADR expands the accounting or payment scope.
- Evolve the existing UI toward a brighter cooperative operations tool using light surfaces and domain-colored status/module accents rather than replacing the frontend stack or copying KOPKARLYTA's exact theme.
