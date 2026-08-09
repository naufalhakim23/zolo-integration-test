-- Order items and sync rows cascade from orders.
DELETE FROM orders
WHERE order_id IN ('ord_998123', 'ord_998124', 'ord_998200', 'ord_998201', 'ord_expired');
