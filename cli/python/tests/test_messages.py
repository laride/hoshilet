import base64
import contextlib
import io
import unittest
from unittest.mock import patch
from hoshilet.messages import decode_message, format_message, ServerMessages
from hoshilet.cli import Client


def encoded(text):
    return base64.b64encode(text.encode()).decode()


class MessageTests(unittest.TestCase):
    def test_validation_and_width(self):
        self.assertEqual(decode_message(encoded('hello\n世界')), 'hello\n世界')
        self.assertEqual(len(decode_message(encoded('a' * 512))), 512)
        for value in [
            'bad-base64',
            encoded('x' * 513),
            encoded('\x1b[31mred'),
            encoded('\x9b31mred'),
            encoded('a\rb'),
            encoded('a\tb'),
            '/w==',
        ]:
            with self.assertRaises(ValueError):
                decode_message(value)
        for width in [1, 3, 20, 80, 100, 150]:
            lines = format_message('first\nsecond', width).splitlines()
            self.assertEqual(len(lines[0]), min(width, 100))
            self.assertEqual(len(lines[-1]), min(width, 100))
            self.assertEqual(lines[1:3], ['first', 'second'])

    def test_stderr_dedup_and_invalid(self):
        stream = io.StringIO()
        with contextlib.redirect_stderr(stream):
            messages = ServerMessages()
            messages.show(encoded('hello'))
            messages.show(encoded('hello'))
            messages.show(encoded('\x1b[2J'))
        self.assertEqual(stream.getvalue().count('hello'), 1)
        self.assertNotIn('\x1b', stream.getvalue())
        self.assertIn('Ignored invalid server message header.', stream.getvalue())

    def test_all_statuses_before_json_decode(self):
        for status, body in [
            (200, b'[]'),
            (400, b'{"error":{"message":"denied"}}'),
            (500, b'not JSON'),
            (302, b'{}'),
        ]:

            class Response:
                def getheader(self, name):
                    return encoded('first\nserver notice')

                def read(self, size):
                    return body

            response = Response()
            response.status = status
            with (
                patch('hoshilet.cli.http.client.HTTPConnection') as conn,
                contextlib.redirect_stderr(io.StringIO()) as stderr,
            ):
                conn.return_value.getresponse.return_value = response
                c = Client('http://127.0.0.1:1234', 'key')
                if status == 200:
                    self.assertEqual(c.request('GET', 'projects'), [])
                else:
                    with self.assertRaises((RuntimeError, ValueError)):
                        c.request('GET', 'projects')
                self.assertIn('server notice', stderr.getvalue())
