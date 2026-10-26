# Read-only MPRIS bridge. Requires distro python3-dbus and python3-gi packages.
import json
import sys

def emit(current=None, error=None):
    print(json.dumps({'current': current, 'error_code': error}), flush=True)

try:
    import dbus
    from dbus.mainloop.glib import DBusGMainLoop
    from gi.repository import GLib
except ImportError:
    emit(error='music_dependency_missing')
    sys.exit(1)

DBusGMainLoop(set_as_default=True)
try:
    bus = dbus.SessionBus()
except dbus.DBusException:
    emit(error='music_session_unavailable')
    sys.exit(1)


def read(*args, **kwargs):
    try:
        names = sorted(n for n in bus.list_names() if n.startswith('org.mpris.MediaPlayer2.'))
        failed = False
        candidates = []
        for name in names:
            try:
                obj = bus.get_object(name, '/org/mpris/MediaPlayer2')
                props = dbus.Interface(obj, 'org.freedesktop.DBus.Properties')
                p = props.GetAll('org.mpris.MediaPlayer2.Player', timeout=2)
                status = str(p.get('PlaybackStatus', ''))
                if status not in ('Playing', 'Paused', 'Stopped'):
                    failed = True
                    continue
                candidates.append((status != 'Playing', name, props, p))
            except dbus.DBusException:
                failed = True
        # Prefer playing sessions; otherwise keep fresh paused/stopped metadata.
        for _, name, props, p in sorted(candidates, key=lambda c: (c[0], c[1])):
            try:
                m = p.get('Metadata', {})
                source = None
                try:
                    source = str(props.Get('org.mpris.MediaPlayer2', 'Identity', timeout=2)) or None
                except dbus.DBusException:
                    pass  # Unknown source never invalidates actual playback.
                artists = m.get('xesam:artist')
                duration = m.get('mpris:length')
                position = p.get('Position')
                emit({'title': str(m['xesam:title']) if 'xesam:title' in m else None,
                      'artist': ', '.join(map(str, artists)) if artists else None,
                      'source': source, 'state': 'playing' if p['PlaybackStatus'] == 'Playing' else 'not_playing',
                      'duration': float(duration)/1000000 if duration is not None else None,
                      'position': float(position)/1000000 if position is not None else None})
                return True
            except dbus.DBusException:
                failed = True
        emit(error='music_read_failed' if failed else None)
    except dbus.DBusException:
        emit(error='music_session_unavailable')
    return True

# Read immediately on metadata/state/source changes; periodic complete reads also
# detect vanished buses and refresh Position (which has no PropertiesChanged).
bus.add_signal_receiver(read, signal_name='PropertiesChanged',
                        dbus_interface='org.freedesktop.DBus.Properties',
                        path='/org/mpris/MediaPlayer2')
def owner_changed(name, old_owner, new_owner):
    if str(name).startswith('org.mpris.MediaPlayer2.'):
        read()

bus.add_signal_receiver(owner_changed, signal_name='NameOwnerChanged',
                        dbus_interface='org.freedesktop.DBus')
GLib.timeout_add_seconds(2, read)
read()
GLib.MainLoop().run()
