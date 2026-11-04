CREATE TABLE restaurants(
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	name       VARCHAR(120)    NOT NULL,
	lat        DECIMAL(9, 6)   NOT NULL,
	lng       DECIMAL(9, 6)   NOT NULL,
	is_active  TINYINT(1)      NOT NULL DEFAULT 1,
	created_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	INDEX idx_rest_geo (lat, lng)
) ENGINE=InnoDB;

CREATE TABLE items(
	id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	restaurant_id BIGINT UNSIGNED NOT NULL,
	name          VARCHAR(120)    NOT NULL,
	sort          INT             NOT NULL DEFAULT 0,
	FOREIGN KEY (restaurant_id) REFERENCES restaurants(id),
	INDEX idx_items_rest (restaurant_id, sort)
) ENGINE=InnoDB;

CREATE TABLE item_sizes(
	item_id     BIGINT UNSIGNED NOT NULL,
	size        VARCHAR(8)      NOT NULL,
	price_paise INT UNSIGNED    NOT NULL,
	PRIMARY KEY (item_id, size),
	FOREIGN KEY (item_id) REFERENCES items(id)
) ENGINE=InnoDB;

CREATE TABLE option_groups(
	id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	item_id       BIGINT UNSIGNED NOT NULL,
	name          VARCHAR(80)     NOT NULL,
	min_select    TINYINT(1)      NOT NULL DEFAULT 0,
	max_select    TINYINT(1)      NOT NULL DEFAULT 1,
	FOREIGN KEY (item_id) REFERENCES items(id)
) ENGINE=InnoDB;

CREATE TABLE options(
	id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
	group_id   BIGINT UNSIGNED NOT NULL,
	name       VARCHAR(80)     NOT NULL,
	in_stock   TINYINT(1)      NOT NULL DEFAULT 1,
	FOREIGN KEY (group_id) REFERENCES option_groups(id)
) ENGINE=InnoDB;

CREATE TABLE option_prices(
	option_id   BIGINT UNSIGNED NOT NULL,
	size        VARCHAR(8)      NOT NULL,
	price_paise INT UNSIGNED    NOT NULL,
	PRIMARY KEY (option_id, size),
	FOREIGN KEY (option_id) REFERENCES options(id)
) ENGINE=InnoDB;

