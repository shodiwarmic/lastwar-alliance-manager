-- +goose Up
-- +goose StatementBegin
-- Update storm_slot_times to match the hardcoded STORM_SLOTS in storm.js
UPDATE storm_slot_times SET label = 'Early',   time_st = '09:00' WHERE slot = 1;
UPDATE storm_slot_times SET label = 'Evening',  time_st = '18:00' WHERE slot = 2;
UPDATE storm_slot_times SET label = 'Late',     time_st = '23:00' WHERE slot = 3;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
UPDATE storm_slot_times SET label = 'Slot 1', time_st = '00:00' WHERE slot = 1;
UPDATE storm_slot_times SET label = 'Slot 2', time_st = '00:00' WHERE slot = 2;
UPDATE storm_slot_times SET label = 'Slot 3', time_st = '00:00' WHERE slot = 3;
-- +goose StatementEnd
