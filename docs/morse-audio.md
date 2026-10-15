# Morse audio contract

The initial profile matches Vxpipe's local Morse codec: 700 Hz sine, amplitude
4096, 60 ms dot, mono signed 16-bit little-endian PCM. Supported rates are 8000,
16000, 24000 and 48000 Hz at the API boundary. Dots/dashes are 1/3 units; symbol,
letter and word gaps are 1/3/7 units. Synthesis appends 14 units of silence.

The alphabet contains A–Z, digits and `. , : ? ' - / ( ) " = + @`. Text is
uppercased and whitespace collapsed. Unsupported characters fail before any audio
is sent. The digit/letter and punctuation mappings follow ITU-R M.1677-1, using
the same directly represented ASCII subset as Vxpipe. This does not recognize
ordinary speech or represent every procedural Morse signal.

The encoder validates text length and rendered duration before producing PCM,
then renders lazily into caller-sized chunks. Arbitrary byte reads, including
split samples, produce identical audio. No full synthesized audio buffer is
required. Oscillator phase restarts at the beginning of each mark.

The decoder retains an analysis window, up to eight pending marks and bounded
transcript/alignment data. Its 10-ms detector checks signal energy and zero
crossing frequency, allowing 100 Hz frequency and one-window timing tolerance.
Majority-active transition windows avoid falsely lengthening marks at sample
rates whose windows do not contain an integral number of dot periods. This is
a controlled-signal decoder; noisy acoustic/microphone performance is not claimed.

An explicit flush finalizes a valid last mark once. Silence never creates text;
odd final sample bytes, invalid mark/gap durations, unknown symbols, wrong
frequencies and duration/text limits fail explicitly. Automatic mode finalizes
after a 14-unit gap; manual mode accumulates words until explicit flush. Absolute
sample positions supply character/word timing (within detector resolution).

The HTTP upload handler streams multipart parts into at most 12 MiB of in-memory
file data, regardless of part ordering; it never calls disk-spilling multipart
helpers. Recognizer processing then consumes bounded chunks. Headers/fields,
upload size, decoded duration and transcript length have independent limits.

Verification combines a handwritten SOS 2 timing sequence rendered as square
waves, expected ET A run lengths, randomized chunk boundaries at all rates and
bidirectional Vxpipe interoperability. A same-codec round trip is additional
evidence rather than the sole oracle. Reference:
[ITU-R M.1677-1](https://www.itu.int/rec/R-REC-M.1677-1-200910-I/en).
