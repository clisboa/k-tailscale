#pragma once
#include <QString>
namespace tailui {
constexpr const char *kStateDir="/mnt/onboard/.adds/tailscale-ui";
QString redact(const QString &message);
void log(const QString &message);
} // namespace tailui
