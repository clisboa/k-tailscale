// Bounded file logging adapted from Relio, Apache-2.0. See NOTICE.
#include "log.h"
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QRegExp>
#include <QTextStream>

namespace tailui {
QString redact(const QString &message) {
    QString out=message.left(2048);
    out.replace(QRegExp("https?://[^\\s\"<>]+",Qt::CaseInsensitive),"[link redacted]");
    out.replace(QRegExp("tskey-[A-Za-z0-9_-]+"),"[credential redacted]");
    out.replace(QRegExp("(privkey|nodekey|machinekey|authkey):[A-Za-z0-9]+",Qt::CaseInsensitive),"[key redacted]");
    return out;
}
void log(const QString &message) {
    const QString logDir="/tmp/kobo-tailscale";
    QDir().mkpath(logDir);QFile::setPermissions(logDir,QFile::ReadOwner|QFile::WriteOwner|QFile::ExeOwner);
    QFile file(logDir+"/plugin.log");
    const bool full=file.exists()&&file.size()>64*1024;
    if(!file.open(QIODevice::WriteOnly|QIODevice::Text|(full?QIODevice::Truncate:QIODevice::Append)))return;
    file.setPermissions(QFile::ReadOwner|QFile::WriteOwner);
    QTextStream(&file)<<QDateTime::currentDateTime().toString(Qt::ISODate)<<' '<<redact(message)<<'\n';
}
} // namespace tailui
