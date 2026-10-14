# HTTP field inventory

Pinned field inventory. Status is pending unless the operation is marked implemented in the compatibility ledger. Each field/default below remains a final acceptance obligation; an endpoint-only smoke is insufficient.

## GET /v1/models

- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## POST /v1/single-use-token/{token_type}

- `path.token_type`: required; schema `{"$ref":"#/components/schemas/SingleUseTokenType"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## POST /v1/speech-to-text

- `query.token`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Token"}`.
- `query.enable_logging`: optional; schema `{"default":true,"title":"Enable request logging.","type":"boolean"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Body: `multipart/form-data`.

- `additional_formats`: optional; schema `{"$ref":"#/components/schemas/AdditionalFormats"}`.
- `cloud_storage_url`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"deprecated":true,"title":"Cloud Storage Url","x-fern-ignore":true}`.
- `detect_speaker_roles`: optional; schema `{"default":false,"title":"Detect Speaker Roles","type":"boolean"}`.
- `diarization_threshold`: optional; schema `{"anyOf":[{"maximum":0.4,"minimum":0.1,"type":"number"},{"type":"null"}],"title":"Diarization Threshold"}`.
- `diarize`: optional; schema `{"default":false,"title":"Diarize","type":"boolean"}`.
- `entity_detection`: optional; schema `{"anyOf":[{"type":"string"},{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Entity Detection"}`.
- `entity_redaction`: optional; schema `{"anyOf":[{"type":"string"},{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Entity Redaction"}`.
- `entity_redaction_mode`: optional; schema `{"default":"enumerated_entity_type","title":"Entity Redaction Mode","type":"string"}`.
- `file`: optional; schema `{"anyOf":[{"format":"binary","type":"string"},{"type":"null"}],"title":"File"}`.
- `file_format`: optional; schema `{"default":"other","enum":["pcm_s16le_16","other"],"title":"File Format","type":"string"}`.
- `keyterms`: optional; schema `{"default":[],"items":{"type":"string"},"title":"Keyterms","type":"array"}`.
- `language_code`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Language Code"}`.
- `model_id`: required; schema `{"title":"Model Id","type":"string"}`.
- `multichannel_output_style`: optional; schema `{"default":"separate","enum":["separate","combined"],"title":"Multichannel Output Style","type":"string"}`.
- `no_verbatim`: optional; schema `{"default":false,"title":"No Verbatim","type":"boolean"}`.
- `num_speakers`: optional; schema `{"anyOf":[{"maximum":32.0,"minimum":1.0,"type":"integer"},{"type":"null"}],"title":"Num Speakers"}`.
- `seed`: optional; schema `{"anyOf":[{"maximum":2147483647.0,"minimum":0.0,"type":"integer"},{"type":"null"}],"title":"Seed"}`.
- `source_url`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Source Url"}`.
- `tag_audio_events`: optional; schema `{"default":true,"title":"Tag Audio Events","type":"boolean"}`.
- `temperature`: optional; schema `{"anyOf":[{"maximum":2.0,"minimum":0.0,"type":"number"},{"type":"null"}],"title":"Temperature"}`.
- `timestamps_granularity`: optional; schema `{"default":"word","enum":["none","word","character"],"title":"Timestamps Granularity","type":"string"}`.
- `transcript_edit`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Transcript Edit"}`.
- `use_multi_channel`: optional; schema `{"default":false,"title":"Use Multi Channel","type":"boolean"}`.
- `use_speaker_library`: optional; schema `{"default":false,"title":"Use Speaker Library","type":"boolean"}`.
- `webhook`: optional; schema `{"default":false,"title":"Webhook","type":"boolean"}`.
- `webhook_id`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Webhook Id"}`.
- `webhook_metadata`: optional; schema `{"anyOf":[{"type":"string"},{"additionalProperties":true,"type":"object"},{"type":"null"}],"title":"Webhook Metadata"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## DELETE /v1/speech-to-text/transcripts/{transcription_id}

- `path.transcription_id`: required; schema `{"title":"Transcription Id","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## GET /v1/speech-to-text/transcripts/{transcription_id}

- `path.transcription_id`: required; schema `{"title":"Transcription Id","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## POST /v1/text-to-speech/{voice_id}

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `query.enable_logging`: optional; schema `{"default":true,"title":"Enable request logging.","type":"boolean"}`.
- `query.optimize_streaming_latency`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"deprecated":true,"title":"DEPRECATED. How much we should optimize streaming request latency (time to first audio byte)."}`.
- `query.output_format`: optional; schema `{"default":"mp3_44100_128","enum":["alaw_8000","mp3_22050_32","mp3_24000_48","mp3_44100_128","mp3_44100_192","mp3_44100_32","mp3_44100_64","mp3_44100_96","opus_48000_128","opus_48000_192","opus_48000_32","opus_48000_64","opus_48000_96","pcm_16000","pcm_22050","pcm_24000","pcm_32000","pcm_44100","pcm_48000","pcm_8000","ulaw_8000","wav_16000","wav_22050","wav_24000","wav_32000","wav_44100","wav_48000","wav_8000"],"title":"Output format of the generated audio.","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Body: `application/json`.

- `apply_language_text_normalization`: optional; schema `{"default":false,"title":"Apply Language Text Normalization","type":"boolean"}`.
- `apply_text_normalization`: optional; schema `{"default":"auto","enum":["auto","on","off"],"title":"Apply Text Normalization","type":"string"}`.
- `audio_effects`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/EffectsSpec-Input"},{"type":"null"}]}`.
- `language_code`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Language Code"}`.
- `model_id`: optional; schema `{"default":"eleven_multilingual_v2","title":"Model Id","type":"string"}`.
- `next_request_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Next Request Ids"}`.
- `next_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Next Text"}`.
- `previous_request_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Previous Request Ids"}`.
- `previous_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Previous Text"}`.
- `pronunciation_dictionary_locators`: optional; schema `{"anyOf":[{"items":{"$ref":"#/components/schemas/PronunciationDictionaryVersionLocatorRequestModel"},"type":"array"},{"type":"null"}],"title":"Pronunciation Dictionary Locators"}`.
- `seed`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"title":"Seed"}`.
- `text`: required; schema `{"title":"Text","type":"string"}`.
- `use_pvc_as_ivc`: optional; schema `{"default":false,"title":"Use Pvc As Ivc","type":"boolean"}`.
- `voice_settings`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/VoiceSettingsResponseModel"},{"type":"null"}]}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## POST /v1/text-to-speech/{voice_id}/stream

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `query.enable_logging`: optional; schema `{"default":true,"title":"Enable request logging.","type":"boolean"}`.
- `query.optimize_streaming_latency`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"deprecated":true,"title":"DEPRECATED. How much we should optimize streaming request latency (time to first audio byte)."}`.
- `query.output_format`: optional; schema `{"default":"mp3_44100_128","enum":["mp3_22050_32","mp3_24000_48","mp3_44100_32","mp3_44100_64","mp3_44100_96","mp3_44100_128","mp3_44100_192","pcm_8000","pcm_16000","pcm_22050","pcm_24000","pcm_32000","pcm_44100","pcm_48000","ulaw_8000","alaw_8000","opus_48000_32","opus_48000_64","opus_48000_96","opus_48000_128","opus_48000_192"],"title":"Output format of the generated audio.","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Body: `application/json`.

- `apply_language_text_normalization`: optional; schema `{"default":false,"title":"Apply Language Text Normalization","type":"boolean"}`.
- `apply_text_normalization`: optional; schema `{"default":"auto","enum":["auto","on","off"],"title":"Apply Text Normalization","type":"string"}`.
- `audio_effects`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/EffectsSpec-Input"},{"type":"null"}]}`.
- `language_code`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Language Code"}`.
- `model_id`: optional; schema `{"default":"eleven_multilingual_v2","title":"Model Id","type":"string"}`.
- `next_request_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Next Request Ids"}`.
- `next_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Next Text"}`.
- `previous_request_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Previous Request Ids"}`.
- `previous_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Previous Text"}`.
- `pronunciation_dictionary_locators`: optional; schema `{"anyOf":[{"items":{"$ref":"#/components/schemas/PronunciationDictionaryVersionLocatorRequestModel"},"type":"array"},{"type":"null"}],"title":"Pronunciation Dictionary Locators"}`.
- `seed`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"title":"Seed"}`.
- `text`: required; schema `{"title":"Text","type":"string"}`.
- `use_pvc_as_ivc`: optional; schema `{"default":false,"title":"Use Pvc As Ivc","type":"boolean"}`.
- `voice_settings`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/VoiceSettingsResponseModel"},{"type":"null"}]}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## POST /v1/text-to-speech/{voice_id}/stream/with-timestamps

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `query.enable_logging`: optional; schema `{"default":true,"title":"Enable request logging.","type":"boolean"}`.
- `query.optimize_streaming_latency`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"deprecated":true,"title":"DEPRECATED. How much we should optimize streaming request latency (time to first audio byte)."}`.
- `query.output_format`: optional; schema `{"default":"mp3_44100_128","enum":["mp3_22050_32","mp3_24000_48","mp3_44100_32","mp3_44100_64","mp3_44100_96","mp3_44100_128","mp3_44100_192","pcm_8000","pcm_16000","pcm_22050","pcm_24000","pcm_32000","pcm_44100","pcm_48000","ulaw_8000","alaw_8000","opus_48000_32","opus_48000_64","opus_48000_96","opus_48000_128","opus_48000_192"],"title":"Output format of the generated audio.","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Body: `application/json`.

- `apply_language_text_normalization`: optional; schema `{"default":false,"title":"Apply Language Text Normalization","type":"boolean"}`.
- `apply_text_normalization`: optional; schema `{"default":"auto","enum":["auto","on","off"],"title":"Apply Text Normalization","type":"string"}`.
- `audio_effects`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/EffectsSpec-Input"},{"type":"null"}]}`.
- `language_code`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Language Code"}`.
- `model_id`: optional; schema `{"default":"eleven_multilingual_v2","title":"Model Id","type":"string"}`.
- `next_request_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Next Request Ids"}`.
- `next_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Next Text"}`.
- `previous_request_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Previous Request Ids"}`.
- `previous_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Previous Text"}`.
- `pronunciation_dictionary_locators`: optional; schema `{"anyOf":[{"items":{"$ref":"#/components/schemas/PronunciationDictionaryVersionLocatorRequestModel"},"type":"array"},{"type":"null"}],"title":"Pronunciation Dictionary Locators"}`.
- `seed`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"title":"Seed"}`.
- `text`: required; schema `{"title":"Text","type":"string"}`.
- `use_pvc_as_ivc`: optional; schema `{"default":false,"title":"Use Pvc As Ivc","type":"boolean"}`.
- `voice_settings`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/VoiceSettingsResponseModel"},{"type":"null"}]}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## POST /v1/text-to-speech/{voice_id}/with-timestamps

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `query.enable_logging`: optional; schema `{"default":true,"title":"Enable request logging.","type":"boolean"}`.
- `query.optimize_streaming_latency`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"deprecated":true,"title":"DEPRECATED. How much we should optimize streaming request latency (time to first audio byte)."}`.
- `query.output_format`: optional; schema `{"default":"mp3_44100_128","enum":["alaw_8000","mp3_22050_32","mp3_24000_48","mp3_44100_128","mp3_44100_192","mp3_44100_32","mp3_44100_64","mp3_44100_96","opus_48000_128","opus_48000_192","opus_48000_32","opus_48000_64","opus_48000_96","pcm_16000","pcm_22050","pcm_24000","pcm_32000","pcm_44100","pcm_48000","pcm_8000","ulaw_8000","wav_16000","wav_22050","wav_24000","wav_32000","wav_44100","wav_48000","wav_8000"],"title":"Output format of the generated audio.","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Body: `application/json`.

- `apply_language_text_normalization`: optional; schema `{"default":false,"title":"Apply Language Text Normalization","type":"boolean"}`.
- `apply_text_normalization`: optional; schema `{"default":"auto","enum":["auto","on","off"],"title":"Apply Text Normalization","type":"string"}`.
- `audio_effects`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/EffectsSpec-Input"},{"type":"null"}]}`.
- `language_code`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Language Code"}`.
- `model_id`: optional; schema `{"default":"eleven_multilingual_v2","title":"Model Id","type":"string"}`.
- `next_request_ids`: optional; schema `{"items":{"type":"string"},"title":"Next Request Ids","type":"array"}`.
- `next_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Next Text"}`.
- `previous_request_ids`: optional; schema `{"items":{"type":"string"},"title":"Previous Request Ids","type":"array"}`.
- `previous_text`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Previous Text"}`.
- `pronunciation_dictionary_locators`: optional; schema `{"items":{"$ref":"#/components/schemas/PronunciationDictionaryVersionLocatorRequestModel"},"title":"Pronunciation Dictionary Locators","type":"array"}`.
- `seed`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"title":"Seed"}`.
- `text`: required; schema `{"title":"Text","type":"string"}`.
- `use_pvc_as_ivc`: optional; schema `{"default":false,"title":"Use Pvc As Ivc","type":"boolean"}`.
- `voice_settings`: optional; schema `{"anyOf":[{"$ref":"#/components/schemas/VoiceSettingsResponseModel"},{"type":"null"}]}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## GET /v1/voices

- `query.show_legacy`: optional; schema `{"anyOf":[{"type":"boolean"},{"type":"null"}],"default":false,"title":"Show Legacy"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## GET /v1/voices/settings/default


Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## DELETE /v1/voices/{voice_id}

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## GET /v1/voices/{voice_id}

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `query.with_settings`: optional; schema `{"default":true,"deprecated":true,"title":"With Settings","type":"boolean"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## GET /v1/voices/{voice_id}/settings

- `path.voice_id`: required; schema `{"title":"Voice Id","type":"string"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.

## GET /v2/voices

- `query.next_page_token`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Next Page Token"}`.
- `query.page_size`: optional; schema `{"default":10,"title":"Page Size","type":"integer"}`.
- `query.search`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Search"}`.
- `query.sort`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Sort"}`.
- `query.sort_direction`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Sort Direction"}`.
- `query.voice_type`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Voice Type"}`.
- `query.category`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Category"}`.
- `query.fine_tuning_state`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Fine Tuning State"}`.
- `query.collection_id`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Collection Id"}`.
- `query.gender`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Gender"}`.
- `query.age`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Age"}`.
- `query.language`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Language"}`.
- `query.accent`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Accent"}`.
- `query.use_cases`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Use Cases"}`.
- `query.min_notice_period_days`: optional; schema `{"anyOf":[{"type":"integer"},{"type":"null"}],"title":"Min Notice Period Days"}`.
- `query.include_custom_rates`: optional; schema `{"anyOf":[{"type":"boolean"},{"type":"null"}],"title":"Include Custom Rates"}`.
- `query.include_live_moderated`: optional; schema `{"anyOf":[{"type":"boolean"},{"type":"null"}],"title":"Include Live Moderated"}`.
- `query.high_quality`: optional; schema `{"anyOf":[{"type":"boolean"},{"type":"null"}],"title":"High Quality"}`.
- `query.include_total_count`: optional; schema `{"default":true,"title":"Include Total Count","type":"boolean"}`.
- `query.voice_ids`: optional; schema `{"anyOf":[{"items":{"type":"string"},"type":"array"},{"type":"null"}],"title":"Voice Ids"}`.
- `header.xi-api-key`: optional; schema `{"anyOf":[{"type":"string"},{"type":"null"}],"title":"Xi-Api-Key"}`.

Response status/schema: see pinned operation in `contracts/elevenlabs-http.json`.
