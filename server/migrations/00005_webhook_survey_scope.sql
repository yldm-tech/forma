-- A webhook's listed surveys outlive the surveys.
--
-- forma2 kept a webhook's surveyIds as plain ids, so a deleted survey stayed in the list and the webhook kept firing for none of the others; the v1 API (Zapier, Make, n8n) still reads that list, where an empty one means every survey. The baseline's foreign key from webhook_surveys to surveys cascaded a survey's deletion into the list and left a webhook scoped to its deleted surveys reading back as an empty one. Survey ids are only ever checked against the webhook's workspace when they are written.

-- +goose Up

ALTER TABLE webhook_surveys DROP CONSTRAINT webhook_surveys_survey_id_fkey;

-- +goose Down

DELETE FROM webhook_surveys ws WHERE NOT EXISTS (SELECT 1 FROM surveys s WHERE s.id = ws.survey_id);
ALTER TABLE webhook_surveys ADD CONSTRAINT webhook_surveys_survey_id_fkey FOREIGN KEY (survey_id) REFERENCES surveys (id) ON DELETE CASCADE;
