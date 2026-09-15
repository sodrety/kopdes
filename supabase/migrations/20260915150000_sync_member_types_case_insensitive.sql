-- PostgreSQL mirror of runtime migration 25 in internal/app/migrations.go.
BEGIN;
SET LOCAL statement_timeout = '30s';

UPDATE public.members
SET member_type = 'employee'
WHERE LOWER(member_no) IN (
    'kksuk-000001', 'kksuk-000002', 'kksuk-000003', 'kksuk-000004', 'kksuk-000005', 'kksuk-000006', 'kksuk-000007', 'kksuk-000008', 'kksuk-000009', 'kksuk-000010', 'kksuk-000011', 'kksuk-000012', 'kksuk-000013', 'kksuk-000014', 'kksuk-000015', 'kksuk-000016', 'kksuk-000017', 'kksuk-000018', 'kksuk-000019', 'kksuk-000020', 'kksuk-000021', 'kksuk-000022', 'kksuk-000023', 'kksuk-000024', 'kksuk-000025', 'kksuk-000026', 'kksuk-000027', 'kksuk-000028', 'kksuk-000029', 'kksuk-000030', 'kksuk-000031', 'kksuk-000032', 'kksuk-000033', 'kksuk-000034', 'kksuk-000035', 'kksuk-000036', 'kksuk-000037', 'kksuk-000038', 'kksuk-000039', 'kksuk-000040', 'kksuk-000041', 'kksuk-000042', 'kksuk-000043', 'kksuk-000044', 'kksuk-000045', 'kksuk-000046', 'kksuk-000047', 'kksuk-000048', 'kksuk-000049', 'kksuk-000051', 'kksuk-000052', 'kksuk-000053', 'kksuk-000054', 'kksuk-000055', 'kksuk-000056', 'kksuk-000057', 'kksuk-000058', 'kksuk-000059', 'kksuk-000060', 'kksuk-000061', 'kksuk-000062', 'kksuk-000063', 'kksuk-000064', 'kksuk-000065', 'kksuk-000066', 'kksuk-000067', 'kksuk-000068', 'kksuk-000069', 'kksuk-000070', 'kksuk-000071', 'kksuk-000072', 'kksuk-000073', 'kksuk-000074', 'kksuk-000075', 'kksuk-000076', 'kksuk-000077', 'kksuk-000078', 'kksuk-000079', 'kksuk-000080', 'kksuk-000081', 'kksuk-000082', 'kksuk-000083', 'kksuk-000084', 'kksuk-000085', 'kksuk-000086', 'kksuk-000087', 'kksuk-000088', 'kksuk-000089', 'kksuk-000090', 'kksuk-000091', 'kksuk-000092', 'kksuk-000093', 'kksuk-000094', 'kksuk-000095', 'kksuk-000096', 'kksuk-000100', 'kksuk-000101', 'kksuk-000104', 'kksuk-000106', 'kksuk-000107', 'kksuk-000109', 'kksuk-000110', 'kksuk-000112', 'kksuk-000115', 'kksuk-000117', 'kksuk-000122', 'kksuk-000124', 'kksuk-000125', 'kksuk-000126', 'kksuk-000129', 'kksuk-000130', 'kksuk-000131', 'kksuk-000132', 'kksuk-000133', 'kksuk-000134', 'kksuk-000135', 'kksuk-000137', 'kksuk-000138', 'kksuk-000139', 'kksuk-000140', 'kksuk-000141', 'kksuk-000142', 'kksuk-000147', 'kksuk-000149', 'kksuk-000150', 'kksuk-000151', 'kksuk-000153', 'kksuk-000155', 'kksuk-000161', 'kksuk-000201', 'kksuk-000203', 'kksuk-000204', 'kksuk-000205', 'kksuk-000206'
);

UPDATE public.members
SET member_type = 'contract_worker'
WHERE LOWER(member_no) IN (
    'kksuk-000050', 'kksuk-000097', 'kksuk-000099', 'kksuk-000103', 'kksuk-000108', 'kksuk-000111', 'kksuk-000113', 'kksuk-000114', 'kksuk-000116', 'kksuk-000118', 'kksuk-000119', 'kksuk-000120', 'kksuk-000121', 'kksuk-000123', 'kksuk-000127', 'kksuk-000128', 'kksuk-000136', 'kksuk-000143', 'kksuk-000144', 'kksuk-000145', 'kksuk-000146', 'kksuk-000148', 'kksuk-000152', 'kksuk-000154', 'kksuk-000156', 'kksuk-000157', 'kksuk-000158', 'kksuk-000159', 'kksuk-000160', 'kksuk-000162', 'kksuk-000163', 'kksuk-000164', 'kksuk-000165', 'kksuk-000166', 'kksuk-000167', 'kksuk-000168', 'kksuk-000169', 'kksuk-000170', 'kksuk-000171', 'kksuk-000176', 'kksuk-000188', 'kksuk-000200', 'kksuk-000202', 'kksuk-000208', 'kksuk-000209'
);

UPDATE public.members
SET member_type = 'daily_worker'
WHERE LOWER(member_no) IN (
    'kksuk-000098', 'kksuk-000102', 'kksuk-000105', 'kksuk-000172', 'kksuk-000173', 'kksuk-000177', 'kksuk-000178', 'kksuk-000179', 'kksuk-000180', 'kksuk-000181', 'kksuk-000182', 'kksuk-000183', 'kksuk-000184', 'kksuk-000185', 'kksuk-000186', 'kksuk-000187', 'kksuk-000189', 'kksuk-000190', 'kksuk-000191', 'kksuk-000192', 'kksuk-000193', 'kksuk-000194', 'kksuk-000195', 'kksuk-000196', 'kksuk-000197', 'kksuk-000198', 'kksuk-000199'
);

UPDATE public.members
SET member_type = 'customer'
WHERE LOWER(member_no) IN ('kksuk-000174', 'kksuk-000175', 'kksuk-000207');

INSERT INTO public.schema_migrations (version, name)
VALUES (25, 'sync_member_types_case_insensitive');

COMMIT;
