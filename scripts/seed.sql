-- 100,000 links with codes s000001 ... s100000, so the load test can open links that exist.
INSERT INTO links (code, url)
SELECT 's' || lpad(i::text, 6, '0'), 'https://example.com/page/' || i
FROM generate_series(1, 100000) AS i
ON CONFLICT DO NOTHING;
