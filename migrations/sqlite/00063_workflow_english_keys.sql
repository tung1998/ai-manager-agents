-- The shipped workflows' commands are English (/handoff, /advisor, /council…):
-- a project's copy of one keeps its changes under the new key; one the
-- project already has under the new key keeps the old one as its own.

-- +goose Up
UPDATE workflows SET key = 'handoff', source = replace(source, 'key: giao-lai' || char(10), 'key: handoff' || char(10))
WHERE key = 'giao-lai' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'handoff');
UPDATE workflows SET source_key = 'handoff' WHERE source_key = 'giao-lai';
UPDATE workflows SET key = 'advisor', source = replace(source, 'key: co-van' || char(10), 'key: advisor' || char(10))
WHERE key = 'co-van' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'advisor');
UPDATE workflows SET source_key = 'advisor' WHERE source_key = 'co-van';
UPDATE workflows SET key = 'council-3', source = replace(source, 'key: hoi-dong-3-ben' || char(10), 'key: council-3' || char(10))
WHERE key = 'hoi-dong-3-ben' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'council-3');
UPDATE workflows SET source_key = 'council-3' WHERE source_key = 'hoi-dong-3-ben';
UPDATE workflows SET key = 'council', source = replace(source, 'key: hoi-dong' || char(10), 'key: council' || char(10))
WHERE key = 'hoi-dong' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'council');
UPDATE workflows SET source_key = 'council' WHERE source_key = 'hoi-dong';
UPDATE workflows SET key = 'feature', source = replace(source, 'key: lam-tinh-nang' || char(10), 'key: feature' || char(10))
WHERE key = 'lam-tinh-nang' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'feature');
UPDATE workflows SET source_key = 'feature' WHERE source_key = 'lam-tinh-nang';
UPDATE workflows SET key = 'bugfix', source = replace(source, 'key: sua-bug' || char(10), 'key: bugfix' || char(10))
WHERE key = 'sua-bug' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'bugfix');
UPDATE workflows SET source_key = 'bugfix' WHERE source_key = 'sua-bug';
UPDATE workflows SET key = 'write-content', source = replace(source, 'key: viet-noi-dung' || char(10), 'key: write-content' || char(10))
WHERE key = 'viet-noi-dung' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'write-content');
UPDATE workflows SET source_key = 'write-content' WHERE source_key = 'viet-noi-dung';

-- +goose Down
UPDATE workflows SET key = 'giao-lai', source = replace(source, 'key: handoff' || char(10), 'key: giao-lai' || char(10))
WHERE key = 'handoff' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'giao-lai');
UPDATE workflows SET source_key = 'giao-lai' WHERE source_key = 'handoff';
UPDATE workflows SET key = 'co-van', source = replace(source, 'key: advisor' || char(10), 'key: co-van' || char(10))
WHERE key = 'advisor' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'co-van');
UPDATE workflows SET source_key = 'co-van' WHERE source_key = 'advisor';
UPDATE workflows SET key = 'hoi-dong-3-ben', source = replace(source, 'key: council-3' || char(10), 'key: hoi-dong-3-ben' || char(10))
WHERE key = 'council-3' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'hoi-dong-3-ben');
UPDATE workflows SET source_key = 'hoi-dong-3-ben' WHERE source_key = 'council-3';
UPDATE workflows SET key = 'hoi-dong', source = replace(source, 'key: council' || char(10), 'key: hoi-dong' || char(10))
WHERE key = 'council' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'hoi-dong');
UPDATE workflows SET source_key = 'hoi-dong' WHERE source_key = 'council';
UPDATE workflows SET key = 'lam-tinh-nang', source = replace(source, 'key: feature' || char(10), 'key: lam-tinh-nang' || char(10))
WHERE key = 'feature' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'lam-tinh-nang');
UPDATE workflows SET source_key = 'lam-tinh-nang' WHERE source_key = 'feature';
UPDATE workflows SET key = 'sua-bug', source = replace(source, 'key: bugfix' || char(10), 'key: sua-bug' || char(10))
WHERE key = 'bugfix' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'sua-bug');
UPDATE workflows SET source_key = 'sua-bug' WHERE source_key = 'bugfix';
UPDATE workflows SET key = 'viet-noi-dung', source = replace(source, 'key: write-content' || char(10), 'key: viet-noi-dung' || char(10))
WHERE key = 'write-content' AND NOT EXISTS (SELECT 1 FROM workflows w WHERE w.project_id = workflows.project_id AND w.key = 'viet-noi-dung');
UPDATE workflows SET source_key = 'viet-noi-dung' WHERE source_key = 'write-content';
