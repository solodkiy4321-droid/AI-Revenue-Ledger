func handleError(op string, err error) error {
	if err == nil {
		return nil
	}

	// 1. Запись не найдена (при SELECT)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}

	// 2. Таймауты и отмена запроса
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", op, ErrTimeout)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: operation canceled: %w", op, err)
	}

	// 3. Ошибки правил таблиц PostgreSQL
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // Дубликат (UNIQUE)
			switch pgErr.ConstraintName {
			case "uq_users_tenant_email":
				return ErrEmailAlreadyExists
			case "uq_integrations_tenant_provider":
				return ErrIntegrationAlreadyExists
			default:
				return fmt.Errorf("%s: duplicate (%s): %w", op, pgErr.ConstraintName, ErrConstraintFailed)
			}

		case "23514": // Нарушение списка значений (CHECK)
			if pgErr.ConstraintName == "chk_users_role" {
				return ErrInvalidRole
			}
			return fmt.Errorf("%s: check failed (%s): %w", op, pgErr.ConstraintName, ErrConstraintFailed)

		case "23502": // Пустое обязательное поле (NOT NULL)
			return fmt.Errorf("%s: missing field %s: %w", op, pgErr.ColumnName, ErrConstraintFailed)

		case "23503": // Битая ссылка (FOREIGN KEY)
			return fmt.Errorf("%s: related record not found (%s): %w", op, pgErr.ConstraintName, ErrConstraintFailed)
		}

		// Любая другая системная ошибка PostgreSQL
		return fmt.Errorf("%s: postgres error [%s] %s (detail: %s): %w", op, pgErr.Code, pgErr.Message, pgErr.Detail, err)
	}

	// 4. Сетевые и прочие ошибки
	return fmt.Errorf("%s: %w", op, err)
}