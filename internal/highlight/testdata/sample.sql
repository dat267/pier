-- a comment
SELECT u.id, u.name, COUNT(o.id) AS orders
FROM users u
LEFT JOIN orders o ON o.user_id = u.id
WHERE u.active = true AND u.created_at > DATE '2024-01-01'
GROUP BY u.id, u.name
HAVING COUNT(o.id) > 2
ORDER BY orders DESC
LIMIT 10;

CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    active BOOLEAN DEFAULT true
);

SELECT CAST(x AS DOUBLE PRECISION), SUBSTRING(name FROM 1 FOR 3) FROM t WHERE x IS NOT NULL;
