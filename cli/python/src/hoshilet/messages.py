"""Decode and display bounded, plain-text instance messages without polluting JSON."""

import base64
import binascii
import os
import sys
import unicodedata

HEADER = 'X-Hoshilet-Server-Message'
TITLE = 'Messages from the Current Server Instance'


def decode_message(value):
    if not value or len(value) > 684:
        raise ValueError('Invalid server message')
    try:
        raw = base64.b64decode(value, validate=True)
        if base64.b64encode(raw).decode('ascii') != value or len(raw) > 512:
            raise ValueError('Invalid server message')
        text = raw.decode('utf-8', errors='strict')
    except (ValueError, UnicodeError, binascii.Error) as exc:
        raise ValueError('Invalid server message') from exc
    if any(c != '\n' and unicodedata.category(c) == 'Cc' for c in text):
        raise ValueError('Invalid server message')
    return text


def terminal_width():
    try:
        width = os.get_terminal_size(sys.stderr.fileno()).columns
    except (OSError, ValueError, AttributeError):
        try:
            width = int(os.environ.get('COLUMNS', '80'))
        except ValueError:
            width = 80
    return min(width if width > 0 else 80, 100)


def format_message(text, width):
    width = max(1, min(int(width), 100))
    title = ' ' + TITLE[: max(0, width - 4)] + ' ' if width >= 4 else ''
    remaining = width - len(title)
    start = '=' * (remaining // 2) + title + '=' * (remaining - remaining // 2)
    return start + '\n' + text + ('' if text.endswith('\n') else '\n') + '=' * width + '\n'


class ServerMessages:
    def __init__(self):
        self.seen = set()
        self.warned = False

    def show(self, value):
        if not value:
            return
        try:
            text = decode_message(value)
        except ValueError:
            if not self.warned:
                print('Ignored invalid server message header.', file=sys.stderr)
                self.warned = True
            return
        if text and text not in self.seen:
            self.seen.add(text)
            sys.stderr.write(format_message(text, terminal_width()))
