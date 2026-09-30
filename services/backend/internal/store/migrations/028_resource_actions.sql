ALTER TABLE operations DROP CONSTRAINT operations_operation_type_check;
ALTER TABLE operations ADD CONSTRAINT operations_operation_type_check CHECK (operation_type IN ('sync', 'rollback', 'resource_action'));
